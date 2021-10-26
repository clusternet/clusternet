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

package hub

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterlifecycle"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/hub/approver"
	"github.com/clusternet/clusternet/pkg/hub/deployer"
	"github.com/clusternet/clusternet/pkg/hub/options"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// default number of threads
	DefaultThreadiness = 2
)

// Hub defines configuration for clusternet-hub
type Hub struct {
	options *options.HubServerOptions

	clusternetInformerFactory informers.SharedInformerFactory
	kubeInformerFactory       kubeinformers.SharedInformerFactory

	kubeClient       *kubernetes.Clientset
	clusternetClient *clusternet.Clientset
	clientBuilder    clientbuilder.ControllerClientBuilder

	crrApprover *approver.CRRApprover
	deployer    *deployer.Deployer

	clusterLifecycle *clusterlifecycle.Controller

	recorder record.EventRecorder

	socketConnection bool
	deployerEnabled  bool
}

// NewHub returns a new Hub.
func NewHub(opts *options.HubServerOptions) (*Hub, error) {
	socketConnection := utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection)
	deployerEnabled := utilfeature.DefaultFeatureGate.Enabled(features.Deployer)

	config, err := utils.LoadsKubeConfig(opts.RecommendedOptions.CoreAPI.CoreAPIKubeconfigPath, 10)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: config,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("kube-client"))
	clusternetClient := clusternet.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-client"))

	//deployer.broadcaster.StartStructuredLogging(5)
	broadcaster := record.NewBroadcaster()
	if kubeClient != nil {
		klog.Infof("sending events to api server")
		broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
			Interface: kubeClient.CoreV1().Events(""),
		})
	} else {
		klog.Warningf("no api server defined - no events will be sent to API server.")
	}
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-hub"})

	// creates the informer factory
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, known.DefaultResync)
	clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetClient, known.DefaultResync)
	approver, err := approver.NewCRRApprover(kubeClient, clusternetClient, clusternetInformerFactory,
		kubeInformerFactory, socketConnection)
	if err != nil {
		return nil, err
	}

	var d *deployer.Deployer
	if deployerEnabled {
		d, err = deployer.NewDeployer(config.Host, kubeClient, clusternetClient, clusternetInformerFactory, kubeInformerFactory, recorder)
		if err != nil {
			return nil, err
		}
	}

	clusterLifecycle := clusterlifecycle.NewController(clusternetClient, clusternetInformerFactory.Clusters().V1beta1().ManagedClusters(), recorder)

	hub := &Hub{
		crrApprover:               approver,
		options:                   opts,
		kubeClient:                kubeClient,
		clusternetClient:          clusternetClient,
		clientBuilder:             rootClientBuilder,
		clusternetInformerFactory: clusternetInformerFactory,
		kubeInformerFactory:       kubeInformerFactory,
		socketConnection:          socketConnection,
		deployer:                  d,
		recorder:                  recorder,
		deployerEnabled:           deployerEnabled,
		clusterLifecycle:          clusterLifecycle,
	}
	return hub, nil
}

// Run starts a new HubAPIServer given HubServerOptions
func (hub *Hub) Run(ctx context.Context) error {
	klog.Info("starting clusternet-hub ...")
	config, err := hub.options.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New(
		hub.options.TunnelLogging,
		hub.socketConnection,
		hub.options.RecommendedOptions.Authentication.RequestHeader.ExtraHeaderPrefixes,
		hub.kubeClient,
		hub.clusternetClient,
		hub.clusternetInformerFactory,
		hub.clientBuilder)
	if err != nil {
		return err
	}

	server.GenericAPIServer.AddPostStartHookOrDie("start-shared-informers-controllers",
		func(context genericapiserver.PostStartHookContext) error {
			klog.Infof("starting Clusternet informers ...")
			// Start the informer factories to begin populating the informer caches
			// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
			hub.kubeInformerFactory.Start(context.StopCh)
			hub.clusternetInformerFactory.Start(context.StopCh)
			config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
			// no need to start LoopbackSharedInformerFactory since we don't store anything in this apiserver
			// hub.options.LoopbackSharedInformerFactory.Start(context.StopCh)

			klog.Infof("starting Clusternet controllers ...")
			// waits for all started informers' cache got synced
			hub.kubeInformerFactory.WaitForCacheSync(context.StopCh)
			hub.clusternetInformerFactory.WaitForCacheSync(context.StopCh)
			// TODO: uncomment this when module "k8s.io/apiserver" gets bumped to a higher version.
			// 		supports k8s.io/apiserver version skew (clusternet/clusternet#137)
			// config.GenericConfig.SharedInformerFactory.WaitForCacheSync(context.StopCh)

			go func() {
				hub.crrApprover.Run(DefaultThreadiness, context.StopCh)
			}()

			if hub.deployerEnabled {
				go func() {
					hub.deployer.Run(DefaultThreadiness, context.StopCh)
				}()
			}

			go func() {
				hub.clusterLifecycle.Run(DefaultThreadiness, context.StopCh)
			}()

			select {
			case <-context.StopCh:
			}

			return nil
		},
	)

	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}
