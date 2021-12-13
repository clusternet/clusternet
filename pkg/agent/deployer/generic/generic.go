/*
Copyright 2021 The Clusternet Authors.

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

package generic

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	cacheddiscovery "k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/description"
	resourcecontroller "github.com/clusternet/clusternet/pkg/controllers/apps/resource"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

func (deployer *Deployer) ControllerHasStarted(gvk schema.GroupVersionKind) bool {
	deployer.lock.RLock()
	defer deployer.lock.RUnlock()
	if _, ok := deployer.rsControllers[gvk]; ok {
		return true
	} else {
		return false
	}
}

func (deployer *Deployer) AddController(resource schema.GroupVersionKind, controller *resourcecontroller.Controller) {
	deployer.lock.Lock()
	defer deployer.lock.Unlock()
	if _, ok := deployer.rsControllers[resource]; ok {
		return
	} else {
		deployer.rsControllers[resource] = controller
	}
}

type Deployer struct {
	lock sync.RWMutex
	// syncMode indicates current sync mode
	syncMode clusterapi.ClusterSyncMode
	// whether AppPusher feature gate is enabled
	appPusherEnabled bool

	// dynamic client and discovery RESTMapper to child cluster
	// using credentials in ServiceAccount "clusternet-app-deployer"
	dynamicClient       dynamic.Interface
	discoveryRESTMapper meta.RESTMapper

	// clusternet client to parent cluster
	clusternetClient *clusternetclientset.Clientset

	descLister     applisters.DescriptionLister
	descSynced     cache.InformerSynced
	descController *description.Controller
	rsControllers  map[schema.GroupVersionKind]*resourcecontroller.Controller

	recorder record.EventRecorder
}

func NewDeployer(syncMode clusterapi.ClusterSyncMode, appPusherEnabled bool,
	appDeployerConfig *rest.Config, clusternetClient *clusternetclientset.Clientset,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory,
	recorder record.EventRecorder) (*Deployer, error) {
	childKubeClient, err := kubernetes.NewForConfig(appDeployerConfig)
	if err != nil {
		return nil, err
	}
	dynamicClient, err := dynamic.NewForConfig(appDeployerConfig)
	if err != nil {
		return nil, err
	}

	deployer := &Deployer{
		syncMode:            syncMode,
		appPusherEnabled:    appPusherEnabled,
		dynamicClient:       dynamicClient,
		discoveryRESTMapper: restmapper.NewDeferredDiscoveryRESTMapper(cacheddiscovery.NewMemCacheClient(childKubeClient.Discovery())),
		clusternetClient:    clusternetClient,
		descLister:          clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:          clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		recorder:            recorder,
		rsControllers:       make(map[schema.GroupVersionKind]*resourcecontroller.Controller),
	}

	descController, err := description.NewController(clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().Descriptions(),
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts(),
		deployer.recorder,
		deployer.handleDescription)
	if err != nil {
		return nil, err
	}
	deployer.descController = descController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int, stopCh <-chan struct{}) {
	klog.Info("starting generic deployer...")
	defer klog.Info("shutting generic deployer")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("generic-deployer", stopCh, deployer.descSynced) {
		return
	}

	go deployer.descController.Run(workers, stopCh)

	<-stopCh
}

func (deployer *Deployer) handleDescription(desc *appsapi.Description) error {
	klog.V(5).Infof("handle Description %s", klog.KObj(desc))
	if desc.Spec.Deployer != appsapi.DescriptionGenericDeployer {
		return nil
	}

	if !utils.DeployableByAgent(deployer.syncMode, deployer.appPusherEnabled) {
		klog.V(5).Infof("Description %s is not deployable by agent, skipping syncing", klog.KObj(desc))
		return nil
	}

	if desc.DeletionTimestamp != nil {
		return utils.OffloadDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
			deployer.discoveryRESTMapper, desc, deployer.recorder)
	}

	return deployer.ApplyDescriptionWithInformer(context.TODO(), desc)
}

func (deployer *Deployer) ApplyDescriptionWithInformer(ctx context.Context, desc *appsapi.Description) error {
	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeployed := desc.Spec.Raw
	errCh := make(chan error, len(objectsToBeDeployed))
	for _, object := range objectsToBeDeployed {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
			continue
		} else {
			labels := resource.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[known.ObjectControlledByLabel] = desc.Namespace + "." + desc.Name
			resource.SetLabels(labels)

			wg.Add(1)
			go func(resource *unstructured.Unstructured) {
				defer wg.Done()

				err := utils.ApplyResourceWithRetry(ctx, deployer.dynamicClient, deployer.discoveryRESTMapper, resource)
				if err != nil {
					errCh <- err
				}
				gvk := resource.GroupVersionKind()

				if !deployer.ControllerHasStarted(gvk) {
					restMapping, _ := deployer.discoveryRESTMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
					//add informer
					apiResource := &metav1.APIResource{
						Group:      resource.GroupVersionKind().Group,
						Version:    resource.GroupVersionKind().Version,
						Kind:       resource.GroupVersionKind().Kind,
						Name:       restMapping.Resource.Resource,
						Namespaced: resource.GetNamespace() != "" || restMapping.Scope.Name() != meta.RESTScopeNameRoot,
					}

					descController, err := resourcecontroller.NewController(apiResource, resource.GetNamespace(),
						deployer.clusternetClient, deployer.dynamicClient, deployer.handleResource)
					if err != nil {
						errCh <- err
					}
					//TODO when to recycle resource controller or make they live forever as resource controller cache?
					stopChan := make(chan struct{})
					descController.Run(known.DefaultThreadiness, stopChan)
					deployer.AddController(gvk, descController)
				}
			}(resource)
		}
	}
	wg.Wait()

	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}

	var statusPhase appsapi.DescriptionPhase
	var reason string
	if len(allErrs) > 0 {
		statusPhase = appsapi.DescriptionPhaseFailure
		reason = utilerrors.NewAggregate(allErrs).Error()

		msg := fmt.Sprintf("failed to deploying Description %s: %s", klog.KObj(desc), reason)
		klog.ErrorDepth(5, msg)
		deployer.recorder.Event(desc, corev1.EventTypeWarning, "UnSuccessfullyDeployed", msg)
	} else {
		statusPhase = appsapi.DescriptionPhaseSuccess
		reason = ""

		msg := fmt.Sprintf("Description %s is deployed successfully", klog.KObj(desc))
		klog.V(5).Info(msg)
		deployer.recorder.Event(desc, corev1.EventTypeNormal, "SuccessfullyDeployed", msg)
	}

	// update status
	desc.Status.Phase = statusPhase
	desc.Status.Reason = reason
	_, err := deployer.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(context.TODO(), desc, metav1.UpdateOptions{})

	if len(allErrs) > 0 {
		return utilerrors.NewAggregate(allErrs)
	}
	return err
}

func (deployer *Deployer) handleResource(obj interface{}) error {
	unStr := obj.(*unstructured.Unstructured)
	desc, err := utils.ResolveDescriptionFromResource(deployer.clusternetClient, obj)
	if err != nil {
		return err
	}
	var allErrs []error
	wg := sync.WaitGroup{}
	objectsToBeDeployed := desc.Spec.Raw
	errCh := make(chan error, 1)
	for _, object := range objectsToBeDeployed {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			allErrs = append(allErrs, err)
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			deployer.recorder.Event(desc, corev1.EventTypeWarning, "FailedMarshalingResource", msg)
		} else {
			labels := resource.GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
			labels[known.ObjectControlledByLabel] = desc.Namespace + "." + desc.Name
			resource.SetLabels(labels)
			rawResource := resource.GroupVersionKind().String() + resource.GetNamespace() + resource.GetName()
			currentResource := unStr.GroupVersionKind().String() + unStr.GetNamespace() + unStr.GetName()
			if rawResource == currentResource {
				wg.Add(1)
				go func(resource *unstructured.Unstructured) {
					defer wg.Done()
					err := utils.ApplyResourceWithRetry(context.TODO(), deployer.dynamicClient, deployer.discoveryRESTMapper, resource)
					if err != nil {
						errCh <- err
					}
				}(resource)
				break
			}
		}
	}
	wg.Wait()
	// collect errors
	close(errCh)
	for err := range errCh {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) > 0 {
		return utilerrors.NewAggregate(allErrs)
	}
	return nil
}
