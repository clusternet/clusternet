/*
Copyright 2023 The Clusternet Authors.

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

package controllermanager

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/version"
	apiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	controllermanagerapp "k8s.io/controller-manager/app"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"
	mcsclientset "sigs.k8s.io/mcs-api/pkg/client/clientset/versioned"
	mcsinformers "sigs.k8s.io/mcs-api/pkg/client/informers/externalversions"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllermanager/approver"
	controllercontext "github.com/clusternet/clusternet/pkg/controllermanager/context"
	"github.com/clusternet/clusternet/pkg/controllermanager/deployer"
	"github.com/clusternet/clusternet/pkg/controllermanager/options"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterlifecycle"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterlifecycle/discovery"
	"github.com/clusternet/clusternet/pkg/controllers/mcs"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// ControllerManager defines configuration for controllers
type ControllerManager struct {
	options           *options.ControllerManagerOptions
	SecureServing     *apiserver.SecureServingInfo
	controllerContext *controllercontext.ControllerContext
}

// NewControllerManager returns a new ControllerManager.
func NewControllerManager(opts *options.ControllerManagerOptions) (*ControllerManager, error) {

	config, err := utils.LoadsKubeConfig(&opts.ClientConnection)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: config,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-controller-manager-kube-client"))
	clusternetClient := clusternet.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-controller-manager-client"))
	electionClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-controller-manager-election-client"))
	mcsClient := mcsclientset.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-controller-manager-mcs-client"))

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
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-controller-manager"})

	cm := &ControllerManager{
		options: opts,
		controllerContext: &controllercontext.ControllerContext{
			Opts:                      opts,
			KubeConfig:                config,
			KubeClient:                kubeClient,
			ElectionClient:            electionClient,
			ClusternetClient:          clusternetClient,
			McsClient:                 mcsClient,
			ClusternetInformerFactory: informers.NewSharedInformerFactory(clusternetClient, known.DefaultResync),
			KubeInformerFactory:       kubeinformers.NewSharedInformerFactory(kubeClient, known.DefaultResync),
			McsInformerFactory:        mcsinformers.NewSharedInformerFactory(mcsClient, known.DefaultResync),
			ClientBuilder:             rootClientBuilder,
			EventRecorder:             recorder,
			InformersStarted:          make(chan struct{}),
		},
	}
	return cm, nil
}

// Run starts a new ControllerManager use given ControllerManagerOptions
func (cm *ControllerManager) Run(ctx context.Context) error {
	klog.Info("starting clusternet-controller-manager ...")
	err := cm.options.Config()
	if err != nil {
		return err
	}
	if err = cm.options.SecureServing.ApplyTo(&cm.SecureServing, nil); err != nil {
		return err
	}
	// Start up the metrics and healthz server.
	if cm.options.SecureServing != nil {
		handler := controllermanagerapp.BuildHandlerChain(
			utils.NewHealthzAndMetricsHandler("clusternet-controller-manager", cm.options.DebuggingOptions),
			nil,
			nil,
		)
		if _, _, err = cm.SecureServing.Serve(handler, 0, ctx.Done()); err != nil {
			// fail early for secure handlers, removing the old error loop from above
			return fmt.Errorf("failed to start secure server: %v", err)
		}
	}

	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}

	// create lease for clusternet controllers
	controllerLease, err := leaderelection.NewLeaderElector(*utils.NewLeaderElectionConfigWithDefaultValue(
		curIdentity,
		cm.options.LeaderElection.ResourceName,
		cm.options.LeaderElection.ResourceNamespace,
		cm.options.LeaderElection.LeaseDuration.Duration,
		cm.options.LeaderElection.RenewDeadline.Duration,
		cm.options.LeaderElection.RetryPeriod.Duration,
		cm.controllerContext.ElectionClient,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				klog.Info("leader elected: starting main controllers.")
				cm.run(ctx)
			},
			OnStoppedLeading: func() {
				klog.Error("leader election got lost")
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

	// No leader election, run directly
	if !cm.options.LeaderElection.LeaderElect {
		cm.run(ctx)
		<-ctx.Done()
		return nil
	}
	// leader election run
	controllerLease.Run(ctx)
	<-ctx.Done()
	return nil
}

func (cm *ControllerManager) run(ctx context.Context) {
	if err := cm.controllerContext.StartControllers(ctx, NewControllerInitializers(), ControllersDisabledByDefault); err != nil {
		klog.Fatalf("error starting controllers: %v", err)
	}
	cm.controllerContext.StartShardInformerFactories(ctx)
	close(cm.controllerContext.InformersStarted)
	<-ctx.Done()
}

// ControllersDisabledByDefault is the set of controllers which is disabled by default
var ControllersDisabledByDefault = sets.NewString(
	"",
)

func NewControllerInitializers() controllercontext.Initializers {
	var initializers = make(controllercontext.Initializers)
	initializers["crrapprover"] = startCRRApproveController
	initializers["serviceimport"] = startServiceImportController
	initializers["clusterlifecycle"] = startClusterLifecycleController
	if utilfeature.DefaultFeatureGate.Enabled(features.Deployer) {
		initializers["deployer"] = startDeployerController
	}
	initializers["clusterdiscovery"] = startClusterDiscoveryController
	return initializers
}

func startCRRApproveController(controllerCtx *controllercontext.ControllerContext, ctx context.Context) (bool, error) {
	socketConnectionEnabled := utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection)
	approve, err := approver.NewCRRApprover(
		controllerCtx.KubeClient,
		controllerCtx.ClusternetClient,
		controllerCtx.ClusternetInformerFactory,
		controllerCtx.KubeInformerFactory,
		socketConnectionEnabled,
	)
	if err != nil {
		return false, err
	}
	go approve.Run(controllerCtx.Opts.Threadiness, ctx)
	return true, nil
}

func startDeployerController(controllerCtx *controllercontext.ControllerContext, ctx context.Context) (bool, error) {
	deployer, err := deployer.NewDeployer(
		controllerCtx.KubeConfig.Host,
		controllerCtx.Opts.LeaderElection.ResourceNamespace,
		controllerCtx.Opts.ReservedNamespace,
		controllerCtx.KubeClient,
		controllerCtx.ClusternetClient,
		controllerCtx.ClusternetInformerFactory,
		controllerCtx.KubeInformerFactory,
		controllerCtx.EventRecorder,
		controllerCtx.Opts.AnonymousAuthSupported,
	)
	if err != nil {
		return false, err
	}
	go deployer.Run(controllerCtx.Opts.Threadiness, ctx)
	return true, nil
}

func startClusterLifecycleController(controllerCtx *controllercontext.ControllerContext, ctx context.Context) (bool, error) {
	clusterLifecycle, err := clusterlifecycle.NewController(
		controllerCtx.ClusternetClient,
		controllerCtx.ClusternetInformerFactory.Clusters().V1beta1().ManagedClusters(),
		controllerCtx.ClusternetInformerFactory.Apps().V1alpha1().Bases(),
		controllerCtx.ClusternetInformerFactory.Apps().V1alpha1().Descriptions().Lister(),
		controllerCtx.EventRecorder,
	)
	if err != nil {
		return false, err
	}
	go clusterLifecycle.Run(controllerCtx.Opts.Threadiness, ctx)
	return true, nil
}

func startServiceImportController(controllerCtx *controllercontext.ControllerContext, ctx context.Context) (bool, error) {
	var ver *version.Info
	ver, err := controllerCtx.McsClient.Discovery().ServerVersion()
	if err != nil {
		return false, err
	}
	serviceImportEnabled, err := utils.MultiClusterServiceEnabled(ver.String())
	if err != nil {
		return false, err
	}
	if serviceImportEnabled {
		sic, err2 := mcs.NewServiceImportController(
			controllerCtx.KubeClient,
			controllerCtx.KubeInformerFactory.Discovery().V1().EndpointSlices(),
			controllerCtx.McsClient,
			controllerCtx.McsInformerFactory,
		)
		if err2 != nil {
			return false, err2
		}
		go sic.Run(ctx)
	}
	return true, nil
}

func startClusterDiscoveryController(controllerCtx *controllercontext.ControllerContext, ctx context.Context) (bool, error) {
	if len(controllerCtx.Opts.ClusterAPIKubeconfig) == 0 {
		klog.Warning("will not discovery clusters created by cluster-api providers due to empty kubeconfig path")
		return false, nil
	}
	clusterAPICfg, err := utils.LoadsKubeConfigFromFile(controllerCtx.Opts.ClusterAPIKubeconfig)
	if err != nil {
		return false, err
	}
	clusterDiscovery := discovery.NewController(
		dynamic.NewForConfigOrDie(clusterAPICfg),
		kubernetes.NewForConfigOrDie(clusterAPICfg),
		known.DefaultResync,
	)
	go clusterDiscovery.Run(ctx)
	return true, nil
}
