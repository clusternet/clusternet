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

	"github.com/clusternet/clusternet/pkg/hub/apiserver"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"
	aggregatorclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	aggregatorinformers "k8s.io/kube-aggregator/pkg/client/informers/externalversions"

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

// Hub defines configuration for clusternet-hub
type Hub struct {
	options *options.HubServerOptions

	clusternetInformerFactory informers.SharedInformerFactory
	kubeInformerFactory       kubeinformers.SharedInformerFactory
	aggregatorInformerFactory aggregatorinformers.SharedInformerFactory

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
	config, err := utils.LoadsKubeConfig(&opts.ClientConnection)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: config,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-hub-kube-client"))
	clusternetClient := clusternet.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-hub-client"))

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
	aggregatorInformerFactory := aggregatorinformers.NewSharedInformerFactory(aggregatorclient.
		NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-hub-kube-client")), known.DefaultResync)

	approver, err := approver.NewCRRApprover(kubeClient, clusternetClient, clusternetInformerFactory,
		kubeInformerFactory, socketConnection)
	if err != nil {
		return nil, err
	}

	var d *deployer.Deployer
	if deployerEnabled {
		d, err = deployer.NewDeployer(config.Host, opts.LeaderElection.ResourceNamespace, opts.ReservedNamespace,
			kubeClient, clusternetClient, clusternetInformerFactory, kubeInformerFactory,
			recorder, opts.AnonymousAuthSupported)
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
		aggregatorInformerFactory: aggregatorInformerFactory,
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
		hub.aggregatorInformerFactory,
		hub.clientBuilder,
		hub.options.ReservedNamespace)
	if err != nil {
		return err
	}

	var cancel context.CancelFunc
	ctx, cancel = context.WithCancel(ctx)

	hub.waitForCacheSync(config, ctx.Done())
	hub.runServer(ctx, cancel, server)

	if !hub.options.LeaderElection.LeaderElect {
		hub.runController(ctx.Done())
		return nil
	}

	// leader election is enabled, runCommand via LeaderElector until done and exit.
	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}
	le, err := leaderelection.NewLeaderElector(*utils.NewLeaderElectionConfigWithDefaultValue(
		curIdentity,
		hub.options.LeaderElection.ResourceName,
		hub.options.LeaderElection.ResourceNamespace,
		hub.options.LeaderElection.LeaseDuration.Duration,
		hub.options.LeaderElection.RenewDeadline.Duration,
		hub.options.LeaderElection.RetryPeriod.Duration,
		hub.kubeClient,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				hub.runController(ctx.Done())
			},
			OnStoppedLeading: func() {
				klog.Fatal("leader election got lost")
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == curIdentity {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	))
	if err != nil {
		return err
	}
	le.Run(ctx)
	return nil
}

func (hub *Hub) runServer(ctx context.Context, cancel context.CancelFunc, server *apiserver.HubAPIServer) {
	go func() {
		klog.Info("starting hub server")
		//  It only returns if stopCh is closed or the secure port cannot be listened on initially.
		err := server.GenericAPIServer.PrepareRun().Run(ctx.Done())
		if err != nil {
			klog.Errorf("failed run hub server: %v", err)
			cancel()
			return
		}
	}()
	return
}

func (hub *Hub) waitForCacheSync(config *apiserver.Config, stopCh <-chan struct{}) {
	klog.Infof("starting Clusternet informers ...")
	// Start the informer factories to begin populating the informer caches
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	hub.kubeInformerFactory.Start(stopCh)
	hub.clusternetInformerFactory.Start(stopCh)
	hub.aggregatorInformerFactory.Start(stopCh)
	config.GenericConfig.SharedInformerFactory.Start(stopCh)
	// no need to start LoopbackSharedInformerFactory since we don't store anything in this apiserver
	// hub.options.LoopbackSharedInformerFactory.Start(stopCh)

	// waits for all started informers' cache got synced
	hub.kubeInformerFactory.WaitForCacheSync(stopCh)
	hub.clusternetInformerFactory.WaitForCacheSync(stopCh)
	hub.aggregatorInformerFactory.WaitForCacheSync(stopCh)
	// TODO: uncomment this when module "k8s.io/apiserver" gets bumped to a higher version.
	// 		supports k8s.io/apiserver version skew (clusternet/clusternet#137)
	// config.GenericConfig.SharedInformerFactory.WaitForCacheSync(stopCh)
}

func (hub *Hub) runController(stopCh <-chan struct{}) {
	klog.Infof("starting Clusternet controllers ...")

	go func() {
		hub.crrApprover.Run(hub.options.Threadiness, stopCh)
	}()

	if hub.deployerEnabled {
		go func() {
			hub.deployer.Run(hub.options.Threadiness, stopCh)
		}()
	}

	go func() {
		hub.clusterLifecycle.Run(hub.options.Threadiness, stopCh)
	}()

	select {
	case <-stopCh:
	}
	return
}
