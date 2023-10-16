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

package helm

import (
	"context"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/helmrelease"
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

	// kube client to parent cluster
	parentKubeClient *kubernetes.Clientset
	// clusternet client to parent cluster
	clusternetClient *clusternetclientset.Clientset

	deployCtx *utils.DeployContext

	hrLister   applisters.HelmReleaseLister
	hrSynced   cache.InformerSynced
	descLister applisters.DescriptionLister
	descSynced cache.InformerSynced

	helmReleaseController *helmrelease.Controller

	recorder record.EventRecorder
}

func NewDeployer(
	syncMode clusterapi.ClusterSyncMode,
	appPusherEnabled bool,
	parentKubeClient *kubernetes.Clientset,
	clusternetClient *clusternetclientset.Clientset,
	deployCtx *utils.DeployContext,
	clusternetInformerFactory clusternetinformers.SharedInformerFactory,
	recorder record.EventRecorder,
) (*Deployer, error) {
	deployer := &Deployer{
		syncMode:         syncMode,
		appPusherEnabled: appPusherEnabled,
		parentKubeClient: parentKubeClient,
		clusternetClient: clusternetClient,
		deployCtx:        deployCtx,
		hrLister:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Lister(),
		hrSynced:         clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Informer().HasSynced,
		descLister:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		descSynced:       clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer().HasSynced,
		recorder:         recorder,
	}

	hrController, err := helmrelease.NewController(clusternetClient,
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases(),
		deployer.recorder,
		deployer.handleHelmRelease)
	if err != nil {
		return nil, err
	}
	deployer.helmReleaseController = hrController

	return deployer, nil
}

func (deployer *Deployer) Run(workers int, ctx context.Context) {
	klog.Info("starting helm deployer...")
	defer klog.Info("shutting helm deployer")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("helm-deployer", ctx.Done(), deployer.descSynced, deployer.hrSynced) {
		return
	}

	go deployer.helmReleaseController.Run(workers, ctx)

	<-ctx.Done()
}

func (deployer *Deployer) handleHelmRelease(hr *appsapi.HelmRelease) error {
	klog.V(5).Infof("handle HelmRelease %s", klog.KObj(hr))

	if !utils.DeployableByAgent(deployer.syncMode, deployer.appPusherEnabled) {
		klog.V(5).Infof("HelmRelease %s is not deployable by agent, skipping syncing", klog.KObj(hr))
		return nil
	}

	return utils.ReconcileHelmRelease(
		context.TODO(),
		deployer.deployCtx,
		deployer.parentKubeClient,
		deployer.clusternetClient,
		deployer.hrLister,
		deployer.descLister,
		hr,
		deployer.recorder,
	)
}
