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

	"k8s.io/apimachinery/pkg/api/meta"
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
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusternetinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/utils"
)

type Deployer struct {
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

	return utils.ApplyDescription(context.TODO(), deployer.clusternetClient, deployer.dynamicClient,
		deployer.discoveryRESTMapper, desc, deployer.recorder)
}
