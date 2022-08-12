/*
Copyright 2022 The Clusternet Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package mcs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dixudx/yacht"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	discoveryinformerv1 "k8s.io/client-go/informers/discovery/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	discoverylisterv1 "k8s.io/client-go/listers/discovery/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/mcs-api/pkg/apis/v1alpha1"
	mcsclientset "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsInformers "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions"
	mcsv1alpha1 "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions/apis/v1alpha1"
	alpha1 "sigs.k8s.io/mcs-api/pkg/client/listers/apis/v1alpha1"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme.Scheme))
}

type SeController struct {
	//local msc client
	mcsClientset       *mcsclientset.Clientset
	parentk8sClient    kubernetes.Interface
	mcsInformerFactory mcsInformers.SharedInformerFactory
	// child cluster dedicated namespace
	dedicatedNamespace    string
	serviceExportLister   alpha1.ServiceExportLister
	endpointSlicesLister  discoverylisterv1.EndpointSliceLister
	serviceExportInformer mcsv1alpha1.ServiceExportInformer
	endpointSliceInformer discoveryinformerv1.EndpointSliceInformer
}

func NewSeController(epsInformer discoveryinformerv1.EndpointSliceInformer, mcsClientset *mcsclientset.Clientset,
	mcsInformerFactory mcsInformers.SharedInformerFactory) *SeController {
	seInformer := mcsInformerFactory.Multicluster().V1alpha1().ServiceExports()
	c := &SeController{
		mcsClientset:          mcsClientset,
		mcsInformerFactory:    mcsInformerFactory,
		endpointSlicesLister:  epsInformer.Lister(),
		serviceExportLister:   seInformer.Lister(),
		serviceExportInformer: seInformer,
		endpointSliceInformer: epsInformer,
	}
	return c
}

func (c *SeController) Handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	ctx := context.Background()
	key := obj.(string)
	namespace, seName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid service export key: %s", key))
		return nil, nil
	}

	se, err := c.serviceExportLister.ServiceExports(namespace).Get(seName)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("service export '%s' in work queue no longer exists", key))
			return nil, nil
		}
		return nil, err
	}

	seTerminating := se.DeletionTimestamp != nil

	if !utils.ContainsString(se.Finalizers, known.AppFinalizer) && !seTerminating {
		se.Finalizers = append(se.Finalizers, known.AppFinalizer)
		se, err = c.mcsClientset.MulticlusterV1alpha1().ServiceExports(namespace).Update(context.TODO(),
			se, metav1.UpdateOptions{})
		if err != nil {
			d := time.Second
			return &d, err
		}
	}

	// recycle corresponding endpoint slice in parent cluster.
	if seTerminating {
		if err = c.parentk8sClient.DiscoveryV1().EndpointSlices(c.dedicatedNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: utils.DerivedName(namespace, seName)}).String(),
		}); err != nil {
			// try next time, make sure we clear endpoint slice
			d := time.Second
			return &d, err
		}
		se.Finalizers = utils.RemoveString(se.Finalizers, known.AppFinalizer)
		se, err = c.mcsClientset.MulticlusterV1alpha1().ServiceExports(namespace).Update(context.TODO(),
			se, metav1.UpdateOptions{})
		if err != nil {
			d := time.Second
			return &d, err
		}
		klog.Infof("service export %s has been recycled successfully", se.Name)
		return nil, nil
	}

	var endpointSliceList []*discoveryv1.EndpointSlice
	if endpointSliceList, err = c.endpointSlicesLister.EndpointSlices(namespace).List(
		labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: se.Name})); err != nil {
		if errors.IsNotFound(err) {
			// service may not exist now, try next time.
			d := time.Second
			return &d, err
		}
		return nil, err
	}

	// remove endpoint slices exist in parent but not in child cluster
	localEndpointSliceMap := make(map[string]bool, 0)
	for _, item := range endpointSliceList {
		localEndpointSliceMap[fmt.Sprintf("%s-%s", se.Namespace, item.Name)] = true
	}
	if parentEndpointSliceList, err := c.parentk8sClient.DiscoveryV1().EndpointSlices(c.dedicatedNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{discoveryv1.LabelServiceName: utils.DerivedName(namespace, seName)}).String()}); err == nil {
		for _, item := range parentEndpointSliceList.Items {
			if !localEndpointSliceMap[item.Name] {
				if err = c.parentk8sClient.DiscoveryV1().EndpointSlices(c.dedicatedNamespace).Delete(ctx, item.Name, metav1.DeleteOptions{}); err != nil {
					utilruntime.HandleError(fmt.Errorf("the endpointclise '%s/%s' in parent cluster deleted failed", item.Namespace, item.Name))
					d := time.Second
					return &d, err
				}
			}
		}
	}

	wg := sync.WaitGroup{}
	var allErrs []error
	errCh := make(chan error, len(endpointSliceList))
	for index := range endpointSliceList {
		wg.Add(1)
		slice := endpointSliceList[index].DeepCopy()
		newSlice := constructEndpointSlice(slice, se, c.dedicatedNamespace)
		go func(slice *discoveryv1.EndpointSlice) {
			defer wg.Done()
			if err = ApplyEndPointSliceWithRetry(c.parentk8sClient, slice); err != nil {
				errCh <- err
				klog.Infof("slice %s sync err: %s", slice.Name, err)
			}
		}(newSlice)
	}
	wg.Wait()
	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) > 0 {
		reason := utilerrors.NewAggregate(allErrs).Error()
		msg := fmt.Sprintf("failed to sync endpoint slices of service export %s: %s", klog.KObj(se), reason)
		klog.ErrorDepth(5, msg)
		d := time.Second
		return &d, err
	}
	klog.Infof("service export %s has been synced successfully", se.Name)
	return nil, nil
}

func (c *SeController) Run(ctx context.Context, parentDedicatedKubeConfig *rest.Config, delicatedNamespace string) error {
	c.mcsInformerFactory.Start(ctx.Done())
	// set parent cluster related filed.
	c.dedicatedNamespace = delicatedNamespace

	parentClient := kubernetes.NewForConfigOrDie(parentDedicatedKubeConfig)
	c.parentk8sClient = parentClient

	controller := yacht.NewController("serviceexport").
		WithCacheSynced(c.serviceExportInformer.Informer().HasSynced, c.endpointSliceInformer.Informer().HasSynced).
		WithHandlerFunc(c.Handle)
	c.serviceExportInformer.Informer().AddEventHandler(controller.DefaultResourceEventHandlerFuncs())

	c.endpointSliceInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			endpointSlice := obj.(*discoveryv1.EndpointSlice)
			if serviceName, ok := endpointSlice.Labels[discoveryv1.LabelServiceName]; ok {
				if _, err := c.serviceExportLister.ServiceExports(endpointSlice.Namespace).Get(serviceName); err == nil {
					return true
				}
			}
			return false
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				if se, err := c.getServiceExportFromEndpointSlice(obj); err == nil {
					controller.Enqueue(se)
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				if se, err := c.getServiceExportFromEndpointSlice(newObj); err == nil {
					controller.Enqueue(se)
				}
			},
			DeleteFunc: func(obj interface{}) {
				if se, err := c.getServiceExportFromEndpointSlice(obj); err == nil {
					controller.Enqueue(se)
				}
			},
		},
	})

	controller.Run(ctx)
	<-ctx.Done()
	return nil
}

func (c *SeController) getServiceExportFromEndpointSlice(obj interface{}) (*v1alpha1.ServiceExport, error) {
	slice := obj.(*discoveryv1.EndpointSlice)
	if serviceName, ok := slice.Labels[discoveryv1.LabelServiceName]; ok {
		if se, err := c.serviceExportLister.ServiceExports(slice.Namespace).Get(serviceName); err == nil {
			return se, nil
		}
	}
	return nil, fmt.Errorf("can't get service export from this slice %s/%s", slice.Namespace, slice.Name)
}

// constructEndpointSlice construct a new endpoint slice from local slice.
func constructEndpointSlice(slice *discoveryv1.EndpointSlice, se *v1alpha1.ServiceExport, namespace string) *discoveryv1.EndpointSlice {
	// mutate slice fields before upload to parent cluster.
	newSlice := &discoveryv1.EndpointSlice{}
	newSlice.AddressType = slice.AddressType
	newSlice.Endpoints = slice.Endpoints
	newSlice.Ports = slice.Ports
	newSlice.Labels = make(map[string]string)

	newSlice.Labels[known.LabelServiceName] = se.Name
	newSlice.Labels[known.LabelServiceNameSpace] = se.Namespace
	newSlice.Labels[known.ObjectCreatedByLabel] = known.ClusternetAgentName
	newSlice.Labels[discoveryv1.LabelServiceName] = utils.DerivedName(se.Namespace, se.Name)

	if subNamespace, exist := se.GetLabels()[known.ConfigSubscriptionNamespaceLabel]; exist {
		newSlice.GetLabels()[known.ConfigSubscriptionNamespaceLabel] = subNamespace
	}

	newSlice.Namespace = namespace
	newSlice.Name = fmt.Sprintf("%s-%s", se.Namespace, slice.Name)
	return newSlice
}

// ApplyEndPointSliceWithRetry create or update existed slices.
func ApplyEndPointSliceWithRetry(client kubernetes.Interface, slice *discoveryv1.EndpointSlice) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() (err error) {
		var lastError error
		_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Create(context.TODO(), slice, metav1.CreateOptions{})
		if lastError == nil {
			return nil
		}
		if !errors.IsAlreadyExists(lastError) {
			return lastError
		}

		curObj, err := client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Get(context.TODO(), slice.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		lastError = nil

		if utils.ResourceNeedResync(curObj, slice, false) {
			// try to update slice
			curObj.Ports = slice.Ports
			curObj.Endpoints = slice.Endpoints
			curObj.AddressType = slice.AddressType
			_, lastError = client.DiscoveryV1().EndpointSlices(slice.GetNamespace()).Update(context.TODO(), curObj, metav1.UpdateOptions{})
			if lastError == nil {
				return nil
			}
		}
		return lastError
	})
}
