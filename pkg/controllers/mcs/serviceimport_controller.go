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
	"reflect"
	"sync"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
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

type ServiceImportController struct {
	mcsClientset          *mcsclientset.Clientset
	localk8sClient        kubernetes.Interface
	serviceImportLister   alpha1.ServiceImportLister
	endpointSlicesLister  discoverylisterv1.EndpointSliceLister
	serviceImportInformer mcsv1alpha1.ServiceImportInformer
	endpointSliceInformer discoveryinformerv1.EndpointSliceInformer
	mcsInformerFactory    mcsInformers.SharedInformerFactory
	yachtController       *yacht.Controller
}

func NewServiceImportController(kubeclient kubernetes.Interface, epsInformer discoveryinformerv1.EndpointSliceInformer, mcsClientset *mcsclientset.Clientset,
	mcsInformerFactory mcsInformers.SharedInformerFactory) (*ServiceImportController, error) {
	siInformer := mcsInformerFactory.Multicluster().V1alpha1().ServiceImports()
	sic := &ServiceImportController{
		mcsClientset:          mcsClientset,
		localk8sClient:        kubeclient,
		serviceImportInformer: siInformer,
		serviceImportLister:   siInformer.Lister(),
		endpointSlicesLister:  epsInformer.Lister(),
		endpointSliceInformer: epsInformer,
		mcsInformerFactory:    mcsInformerFactory,
	}

	// add event handler for ServiceImport
	yachtcontroller := yacht.NewController("serviceimport").
		WithCacheSynced(siInformer.Informer().HasSynced, epsInformer.Informer().HasSynced).
		WithHandlerFunc(sic.Handle).
		WithEnqueueFilterFunc(preFilter)
	_, err := siInformer.Informer().AddEventHandler(yachtcontroller.DefaultResourceEventHandlerFuncs())
	if err != nil {
		klog.Fatalf("failed to add event handler for serviceimport: %v", err)
		return nil, err
	}
	_, err = epsInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			if si, err2 := sic.getServiceImportFromEndpointSlice(obj); err2 == nil {
				yachtcontroller.Enqueue(si)
			}
			return false
		},
	})
	if err != nil {
		klog.Fatalf("failed to add event handler for serviceimport: %v", err)
		return nil, err
	}
	sic.yachtController = yachtcontroller
	return sic, nil
}

func (c *ServiceImportController) Handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	ctx := context.Background()
	key := obj.(string)
	namespace, siName, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid service import key: %s", key))
		return nil, nil
	}
	cachedSi, err := c.serviceImportLister.ServiceImports(namespace).Get(siName)
	if err != nil {
		if errors.IsNotFound(err) {
			utilruntime.HandleError(fmt.Errorf("service import '%s' in work queue no longer exists", key))
			return nil, nil
		}
		return nil, err
	}
	si := cachedSi.DeepCopy()
	siTerminating := si.DeletionTimestamp != nil
	rawServiceName := si.Labels[known.LabelServiceName]
	rawServiceNamespace := si.Labels[known.LabelServiceNameSpace]

	if !utils.ContainsString(si.Finalizers, known.AppFinalizer) && !siTerminating {
		si.Finalizers = append(si.Finalizers, known.AppFinalizer)
		si, err = c.mcsClientset.MulticlusterV1alpha1().ServiceImports(namespace).Update(context.TODO(),
			si, metav1.UpdateOptions{})
		if err != nil {
			d := time.Second
			return &d, err
		}
	}
	// recycle corresponding endpoint slice in parent cluster.
	if siTerminating {
		if err = c.recycleServiceImport(ctx, si); err != nil {
			d := time.Second
			return &d, err
		}
		klog.Infof("service import %s has been recycled successfully", si.Name)
		return nil, nil
	}
	// apply service.
	if err = c.applyServiceFromServiceImport(si); err != nil {
		d := time.Second
		return &d, err
	}
	// apply endpoint slices.
	srcLabelMap := labels.Set{
		known.LabelServiceName:      rawServiceName,
		known.LabelServiceNameSpace: rawServiceNamespace,
		known.ObjectCreatedByLabel:  known.ClusternetAgentName,
	}
	dstLabelMap := labels.Set{
		known.LabelServiceName:      rawServiceName,
		known.LabelServiceNameSpace: rawServiceNamespace,
	}

	endpointSliceList, err := utils.RemoveNonexistentEndpointslice(c.endpointSlicesLister, corev1.NamespaceAll,
		srcLabelMap, c.localk8sClient, namespace, dstLabelMap)
	if err != nil {
		d := time.Second
		return &d, err
	}

	// transport endpointslice from delicate ns to target ns.
	wg := sync.WaitGroup{}
	var allErrs []error
	errCh := make(chan error, len(endpointSliceList))
	for index := range endpointSliceList {
		wg.Add(1)
		slice := endpointSliceList[index].DeepCopy()
		newSlice := forkEndpointSlice(slice, namespace)
		go func(slice *discoveryv1.EndpointSlice) {
			defer wg.Done()
			if err = utils.ApplyEndPointSliceWithRetry(c.localk8sClient, slice); err != nil {
				errCh <- err
				klog.Infof("slice %s sync err from %s to %s for: %v", slice.Name, slice.Namespace, namespace, err)
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
		msg := fmt.Sprintf("failed to sync endpoint slices for %s: %s", klog.KObj(si), reason)
		klog.ErrorDepth(5, msg)
		d := time.Second
		return &d, err
	}
	klog.Infof("service import %s has been synced successfully", si.Name)
	return nil, nil
}

// applyServiceFromServiceImport create derived service and update the service status.
func (c *ServiceImportController) applyServiceFromServiceImport(svcImport *v1alpha1.ServiceImport) error {
	rawServiceName, _ := svcImport.Labels[known.LabelServiceName]
	rawServiceNamespace, _ := svcImport.Labels[known.LabelServiceNameSpace]
	newService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: svcImport.Namespace,
			Name:      utils.DerivedName(rawServiceNamespace, rawServiceName),
		},
		Spec: corev1.ServiceSpec{
			Type:  corev1.ServiceTypeClusterIP,
			Ports: servicePorts(svcImport),
		},
	}

	derivedService, err := c.localk8sClient.CoreV1().Services(svcImport.Namespace).Get(context.TODO(), newService.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		derivedService, err = c.localk8sClient.CoreV1().Services(svcImport.Namespace).Create(context.TODO(), newService, metav1.CreateOptions{})
		if err != nil {
			klog.Errorf("Create delicate service(%s/%s) failed, for: %v", newService.Namespace, newService.Name, err)
			return err
		}
	}

	if !reflect.DeepEqual(derivedService.Spec.Ports, newService.Spec.Ports) {
		derivedService.Spec.Ports = newService.Spec.Ports
		if derivedService, err = c.localk8sClient.CoreV1().Services(svcImport.Namespace).Update(context.TODO(), derivedService, metav1.UpdateOptions{}); err != nil {
			klog.Errorf("Update derived service(%s/%s) spec failed, for %v", derivedService.Namespace, derivedService.Name, err)
			return err
		}
	}

	if err = c.updateServiceStatus(svcImport, derivedService); err != nil {
		klog.Errorf("Update derived service(%s/%s) status failed, for %v", newService.Namespace, newService.Name, err)
		return err
	}

	return nil
}

// updateServiceStatus update ServiceStatus with retry.
func (c *ServiceImportController) updateServiceStatus(svcImport *v1alpha1.ServiceImport, derivedService *corev1.Service) error {
	klog.V(5).Infof("try to update Service %q status", derivedService.Name)
	// update loadbalanacer status with provided clusterset IPs
	var ingress []corev1.LoadBalancerIngress
	for _, ip := range svcImport.Spec.IPs {
		ingress = append(ingress, corev1.LoadBalancerIngress{
			IP: ip,
		})
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		derivedService.Status = corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: ingress,
			},
		}
		_, err := c.localk8sClient.CoreV1().Services(derivedService.Namespace).UpdateStatus(context.TODO(), derivedService, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}

		updated, err2 := c.localk8sClient.CoreV1().Services(derivedService.Namespace).Get(context.TODO(),
			derivedService.Name, metav1.GetOptions{})
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			derivedService = updated.DeepCopy()
			return nil
		}
		utilruntime.HandleError(fmt.Errorf("error getting updated Service %q from lister: %v", derivedService.Name,
			err2))
		return err2
	})
}

func (c *ServiceImportController) Run(ctx context.Context) {
	c.mcsInformerFactory.Start(ctx.Done())
	c.yachtController.Run(ctx)
}

// recycleServiceImport recycle derived service and derived endpoint slices.
func (c *ServiceImportController) recycleServiceImport(ctx context.Context, si *v1alpha1.ServiceImport) error {
	rawServiceName := si.Labels[known.LabelServiceName]
	rawServiceNamespace := si.Labels[known.LabelServiceNameSpace]
	// 1. recycle endpoint slices.
	if err := c.localk8sClient.DiscoveryV1().EndpointSlices(si.Namespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set{
			known.LabelServiceName:      rawServiceName,
			known.LabelServiceNameSpace: rawServiceNamespace}).String(),
	}); err != nil {
		// try next time, make sure we clear all related endpoint slices
		return err
	}
	// 2. recycle derived service.
	svcName := utils.DerivedName(rawServiceNamespace, rawServiceName)
	err := c.localk8sClient.CoreV1().Services(si.Namespace).Delete(ctx, svcName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		klog.Errorf("Delete derived service(%s) failed, Error: %v", svcName, err)
		return err
	}

	si.Finalizers = utils.RemoveString(si.Finalizers, known.AppFinalizer)
	_, err = c.mcsClientset.MulticlusterV1alpha1().ServiceImports(si.Namespace).Update(context.TODO(),
		si, metav1.UpdateOptions{})
	return err
}

// getServiceImportFromEndpointSlice get ServiceImport from endpointSlice labels, get the first if more than one.
func (c *ServiceImportController) getServiceImportFromEndpointSlice(obj interface{}) (*v1alpha1.ServiceImport, error) {
	slice := obj.(*discoveryv1.EndpointSlice)
	rawServiceName, serviceExist := slice.Labels[known.LabelServiceName]
	rawServiceNamespace, serviceNamespaceExsit := slice.Labels[known.LabelServiceNameSpace]
	subNamespace, subNamespaceExsit := slice.Labels[known.ConfigSubscriptionNamespaceLabel]
	if serviceExist && serviceNamespaceExsit && subNamespaceExsit {
		if siList, err := c.serviceImportLister.ServiceImports(subNamespace).List(
			labels.SelectorFromSet(labels.Set{
				known.LabelServiceName:      rawServiceName,
				known.LabelServiceNameSpace: rawServiceNamespace,
			})); err == nil && len(siList) > 0 {
			return siList[0], nil
		}
	}

	return nil, fmt.Errorf("can't resolve service import from this slice %s/%s", slice.Namespace, slice.Name)
}

// forkEndpointSlice construct a new endpoint slice from source slice.
func forkEndpointSlice(slice *discoveryv1.EndpointSlice, namespace string) *discoveryv1.EndpointSlice {
	// mutate slice fields before upload to parent cluster.
	newSlice := &discoveryv1.EndpointSlice{
		AddressType: slice.AddressType,
		Endpoints:   slice.Endpoints,
		Ports:       slice.Ports,
	}
	delete(slice.Labels, known.ObjectCreatedByLabel)
	newSlice.Labels = slice.Labels
	newSlice.Namespace = namespace
	newSlice.Name = fmt.Sprintf("%s-%s", slice.Namespace, slice.Name)
	return newSlice
}

// servicePorts get service port from ServiceImport
func servicePorts(svcImport *v1alpha1.ServiceImport) []corev1.ServicePort {
	ports := make([]corev1.ServicePort, len(svcImport.Spec.Ports))
	for i, p := range svcImport.Spec.Ports {
		ports[i] = corev1.ServicePort{
			Name:        p.Name,
			Protocol:    p.Protocol,
			Port:        p.Port,
			AppProtocol: p.AppProtocol,
		}
	}
	return ports
}

// preFilter filter ServiceImport if has no label known.LabelServiceName and known.LabelServiceNameSpace
func preFilter(oldObj, newObj interface{}) (bool, error) {
	var si *v1alpha1.ServiceImport
	if newObj == nil {
		// Delete
		si = oldObj.(*v1alpha1.ServiceImport)
	} else {
		// Add or Update
		si = newObj.(*v1alpha1.ServiceImport)
	}

	if si.Spec.Type != v1alpha1.ClusterSetIP {
		return false, nil
	}
	_, serviceExist := si.Labels[known.LabelServiceName]
	_, serviceNamespaceExist := si.Labels[known.LabelServiceNameSpace]

	if !serviceExist || !serviceNamespaceExist {
		return false, nil
	}

	return true, nil
}
