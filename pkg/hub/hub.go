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
	"time"

	corev1 "k8s.io/api/core/v1"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeInformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"
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
	ctx context.Context

	options *options.HubServerOptions

	clusternetInformerFactory informers.SharedInformerFactory
	kubeInformerFactory       kubeInformers.SharedInformerFactory
	crdInformerFactory        crdinformers.SharedInformerFactory

	kubeclient       *kubernetes.Clientset
	clusternetclient *clusternet.Clientset
	crdclient        *crdclientset.Clientset

	crrApprover *approver.CRRApprover
	deployer    *deployer.Deployer

	clusterLifecycle *clusterlifecycle.Controller

	recorder record.EventRecorder

	socketConnection bool
	deployerEnabled  bool
}

// NewHub returns a new Hub.
func NewHub(ctx context.Context, opts *options.HubServerOptions) (*Hub, error) {
	socketConnection := utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection)
	deployerEnabled := utilfeature.DefaultFeatureGate.Enabled(features.Deployer)

	config, err := utils.LoadsKubeConfig(opts.RecommendedOptions.CoreAPI.CoreAPIKubeconfigPath, 10)
	if err != nil {
		return nil, err
	}

	// creating the clientset
	kubeclient := kubernetes.NewForConfigOrDie(config)
	clusternetclient := clusternet.NewForConfigOrDie(config)
	crdclient := crdclientset.NewForConfigOrDie(config)

	//deployer.broadcaster.StartStructuredLogging(5)
	broadcaster := record.NewBroadcaster()
	if kubeclient != nil {
		klog.Infof("sending events to api server")
		broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
			Interface: kubeclient.CoreV1().Events(""),
		})
	} else {
		klog.Warningf("no api server defined - no events will be sent to API server.")
	}
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-hub"})

	// creates the informer factory
	kubeInformerFactory := kubeInformers.NewSharedInformerFactory(kubeclient, known.DefaultResync)
	clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetclient, known.DefaultResync)
	crdInformerFactory := crdinformers.NewSharedInformerFactory(crdclient, 5*time.Minute)
	approver, err := approver.NewCRRApprover(ctx, kubeclient, clusternetclient, clusternetInformerFactory,
		kubeInformerFactory, socketConnection)
	if err != nil {
		return nil, err
	}

	// add informers for minimum requirements
	// register informers first before informerFactory starts
	kubeInformerFactory.Core().V1().Namespaces().Informer()
	kubeInformerFactory.Core().V1().ServiceAccounts().Informer()
	kubeInformerFactory.Core().V1().Secrets().Informer()
	clusternetInformerFactory.Clusters().V1beta1().ClusterRegistrationRequests().Informer()
	clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer()
	crdInformerFactory.Apiextensions().V1().CustomResourceDefinitions().Informer()

	var d *deployer.Deployer
	if deployerEnabled {
		// register informers first before informerFactory starts
		clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer()
		clusternetInformerFactory.Apps().V1alpha1().Bases().Informer()
		clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer()
		clusternetInformerFactory.Apps().V1alpha1().HelmCharts().Informer()
		clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer()
		clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Informer()
		clusternetInformerFactory.Apps().V1alpha1().Localizations().Informer()
		clusternetInformerFactory.Apps().V1alpha1().Globalizations().Informer()

		d, err = deployer.NewDeployer(ctx, kubeclient, clusternetclient, clusternetInformerFactory, kubeInformerFactory, recorder)
		if err != nil {
			return nil, err
		}
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.ShadowAPI) {
		clusternetInformerFactory.Apps().V1alpha1().Manifests().Informer()
	}

	clusterLifecycle := clusterlifecycle.NewController(ctx, clusternetclient, clusternetInformerFactory.Clusters().V1beta1().ManagedClusters(), recorder)

	hub := &Hub{
		ctx:                       ctx,
		crrApprover:               approver,
		options:                   opts,
		kubeclient:                kubeclient,
		clusternetclient:          clusternetclient,
		crdclient:                 crdclient,
		clusternetInformerFactory: clusternetInformerFactory,
		kubeInformerFactory:       kubeInformerFactory,
		crdInformerFactory:        crdInformerFactory,
		socketConnection:          socketConnection,
		deployer:                  d,
		recorder:                  recorder,
		deployerEnabled:           deployerEnabled,
		clusterLifecycle:          clusterLifecycle,
	}

	// Start the informer factories to begin populating the informer caches
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	kubeInformerFactory.Start(ctx.Done())
	clusternetInformerFactory.Start(ctx.Done())
	crdInformerFactory.Start(ctx.Done())

	return hub, nil
}

func (hub *Hub) Run() error {
	go func() {
		hub.crrApprover.Run(DefaultThreadiness)
	}()

	if hub.deployerEnabled {
		go func() {
			hub.deployer.Run(DefaultThreadiness)
		}()
	}

	go hub.clusterLifecycle.Run(DefaultThreadiness, hub.ctx.Done())

	return hub.RunAPIServer()
}

// RunAPIServer starts a new HubAPIServer given HubServerOptions
func (hub *Hub) RunAPIServer() error {
	klog.Info("starting Clusternet Hub APIServer ...")
	config, err := hub.options.Config()
	if err != nil {
		return err
	}

	server, err := config.Complete().New(hub.options.TunnelLogging, hub.socketConnection,
		hub.options.RecommendedOptions.Authentication.RequestHeader.ExtraHeaderPrefixes,
		hub.kubeclient,
		hub.clusternetclient,
		hub.clusternetInformerFactory)
	if err != nil {
		return err
	}

	//config.Complete().GenericConfig.MaxRequestBodyBytes

	server.GenericAPIServer.AddPostStartHookOrDie("start-clusternet-hub-informers", func(context genericapiserver.PostStartHookContext) error {
		config.GenericConfig.SharedInformerFactory.Start(context.StopCh)
		// no need to start LoopbackSharedInformerFactory since we don't store anything in this apiserver
		// hub.options.LoopbackSharedInformerFactory.Start(context.StopCh)
		return nil
	})

	return server.GenericAPIServer.PrepareRun().Run(hub.ctx.Done())
}
