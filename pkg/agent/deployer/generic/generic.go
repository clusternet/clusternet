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
	"reflect"
	"sync"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

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
	httpClient, err := rest.HTTPClientFor(appDeployerConfig)
	if err != nil {
		return nil, err
	}
	mapper, err := apiutil.NewDynamicRESTMapper(appDeployerConfig, httpClient)
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
		discoveryRESTMapper: mapper,
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

func (deployer *Deployer) Run(workers int, ctx context.Context) {
	klog.Info("starting generic deployer...")
	defer klog.Info("shutting generic deployer")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("generic-deployer", ctx.Done(), deployer.descSynced) {
		return
	}

	go deployer.descController.Run(workers, ctx)

	<-ctx.Done()
}

func (deployer *Deployer) handleDescription(desc *appsapi.Description) error {
	klog.V(5).Infof("handle Description %s", klog.KObj(desc))
	if desc.Spec.Deployer != appsapi.DescriptionGenericDeployer {
		return nil
	}

	if !utils.DeployableByAgent(deployer.syncMode, deployer.appPusherEnabled) {
		klog.V(5).Infof("Description %s is not deployable by agent, skipping syncing", klog.KObj(desc))
		return utils.ApplyDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
			deployer.discoveryRESTMapper, desc, deployer.recorder, true, deployer.ResourceCallbackHandler, false)
	}

	if desc.DeletionTimestamp != nil {
		return utils.OffloadDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
			deployer.discoveryRESTMapper, desc, deployer.recorder)
	}

	return utils.ApplyDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
		deployer.discoveryRESTMapper, desc, deployer.recorder, false, deployer.ResourceCallbackHandler, false)
}

func (deployer *Deployer) ResourceCallbackHandler(resource *unstructured.Unstructured) error {
	gvk := resource.GroupVersionKind()

	if !deployer.ControllerHasStarted(gvk) {
		restMapping, err := deployer.discoveryRESTMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			klog.Errorf("please check whether the advertised apiserver of current child cluster is accessible. %v", err)
			return err
		}

		//add informer
		apiResource := &metav1.APIResource{
			Group:      resource.GroupVersionKind().Group,
			Version:    resource.GroupVersionKind().Version,
			Kind:       resource.GroupVersionKind().Kind,
			Name:       restMapping.Resource.Resource,
			Namespaced: false, // set to false for all namespaces
		}

		resourceController, err := resourcecontroller.NewController(apiResource,
			deployer.clusternetClient, deployer.dynamicClient, deployer.handleResource)
		if err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		//DO NOT recycle resource controller so they live forever as resource controller cache
		deployer.AddController(gvk, resourceController)
		go func() {
			stopChan := make(chan struct{})
			resourceController.Run(known.DefaultThreadiness, stopChan)
		}()

	}
	return nil
}

func (deployer *Deployer) handleResource(resAttrs *resourcecontroller.ResourceAttrs) error {
	klog.Infof("handle handleResource [%s]", resAttrs)
	if resAttrs == nil {
		return fmt.Errorf("unexpected value for resAttrs nil")
	}

	desc, err := deployer.descLister.Descriptions(resAttrs.Namespace).Get(resAttrs.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.V(2).Infof("the owner description %s/%s has been deleted", resAttrs.Namespace, resAttrs.Name)
			return nil
		}
		return err
	}

	if desc.DeletionTimestamp != nil {
		klog.V(4).Infof("do not rollback in-deleting Description %s/%s", resAttrs.Namespace, resAttrs.Name)
		return nil
	}

	if resAttrs.ObjectAction != resourcecontroller.ObjectDelete {
		err = deployer.SyncDescriptionStatus(deployer.dynamicClient, deployer.discoveryRESTMapper, desc)
		if err != nil {
			klog.Errorf("Failed Sync Description Status. %v", err)
			return err
		}
	}

	if resAttrs.ObjectAction != resourcecontroller.ObjectUpdateStatus {
		err = utils.ApplyDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
			deployer.discoveryRESTMapper, desc, deployer.recorder, false, nil, true)
		if err == nil {
			klog.V(4).Infof("successfully rollback Description %s/%s", resAttrs.Namespace, resAttrs.Name)
		}
	}
	return err
}

func (deployer *Deployer) ControllerHasStarted(gvk schema.GroupVersionKind) bool {
	deployer.lock.RLock()
	defer deployer.lock.RUnlock()
	if _, ok := deployer.rsControllers[gvk]; ok {
		return true
	} else {
		return false
	}
}

func (deployer *Deployer) AddController(gvk schema.GroupVersionKind, controller *resourcecontroller.Controller) {
	deployer.lock.Lock()
	defer deployer.lock.Unlock()
	if _, ok := deployer.rsControllers[gvk]; ok {
		return
	} else {
		deployer.rsControllers[gvk] = controller
	}
}

func (deployer *Deployer) SyncDescriptionStatus(
	dynamicClient dynamic.Interface,
	restMapper meta.RESTMapper,
	desc *appsapi.Description,
) error {
	descStatus := desc.Status.DeepCopy()
	// descStatusMap for check and update exsit ManifestStatus
	descStatusMap := make(map[string]int)
	for index, status := range descStatus.ManifestStatuses {
		descStatusMap[status.Namespace+"/"+status.Name] = index
	}

	objectsToBeDeployed := desc.Spec.Raw
	for _, object := range objectsToBeDeployed {
		resource := &unstructured.Unstructured{}
		err := resource.UnmarshalJSON(object)
		if err != nil {
			msg := fmt.Sprintf("failed to unmarshal resource: %v", err)
			klog.ErrorDepth(5, msg)
			continue
		}

		restMapping, err := restMapper.RESTMapping(resource.GroupVersionKind().GroupKind(), resource.GroupVersionKind().Version)
		if err != nil {
			klog.Errorf("please check whether the advertised apiserver of current child cluster is accessible. %v", err)
			return err
		}

		// Get cluster Resource
		resourceObj, err := dynamicClient.Resource(restMapping.Resource).Namespace(resource.GetNamespace()).
			Get(context.TODO(), resource.GetName(), metav1.GetOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to Get resource: %v", err)
			klog.ErrorDepth(5, msg)
			return err
		}
		manifestStatus := appsapi.ManifestStatus{
			Feed: appsapi.Feed{
				Kind:       resourceObj.GetKind(),
				APIVersion: resourceObj.GetAPIVersion(),
				Namespace:  resourceObj.GetNamespace(),
				Name:       resourceObj.GetName(),
			},
		}
		statusMap, _, err := unstructured.NestedMap(resourceObj.Object, "status")
		if err != nil {
			klog.Errorf("Failed to get status field from %s(%s/%s), error: %v", resourceObj.GetKind(),
				resourceObj.GetNamespace(), resourceObj.GetName(), err)
			return err
		}
		result := &unstructured.Unstructured{}
		result.SetUnstructuredContent(statusMap)

		manifestStatus.ObservedStatus.Reset()
		manifestStatus.ObservedStatus.Object = result.DeepCopyObject()

		key := manifestStatus.Namespace + "/" + manifestStatus.Name
		if index, ok := descStatusMap[key]; ok {
			descStatus.ManifestStatuses[index] = *manifestStatus.DeepCopy()
		} else {
			descStatus.ManifestStatuses = append(descStatus.ManifestStatuses, *manifestStatus.DeepCopy())
			descStatusMap[key] = len(descStatus.ManifestStatuses) - 1
		}
	}

	// try to update Descriptions Status
	var err error
	if !reflect.DeepEqual(desc.Status.ManifestStatuses, descStatus.ManifestStatuses) {
		err = utils.UpdateDescriptionStatus(context.TODO(), desc, descStatus, deployer.clusternetClient, false)
		klog.V(5).Infof("SyncDescriptionStatus Descriptions manifestStatus has changed, UpdateStatus. err: %s", err)
	}

	return err
}
