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
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/rancher/remotedialer"
	coordinationapi "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	crdclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	crdinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	kubeinformers "k8s.io/client-go/informers"
	coordinationinformers "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	componentbaseconfig "k8s.io/component-base/config"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"
	aggregatorclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	aggregatorinformers "k8s.io/kube-aggregator/pkg/client/informers/externalversions"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/misc/leasegc"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	shadowapiserver "github.com/clusternet/clusternet/pkg/hub/apiserver/shadow"
	_ "github.com/clusternet/clusternet/pkg/hub/metrics"
	"github.com/clusternet/clusternet/pkg/hub/options"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// peerLeasePrefix specifies the prefix name for clusternet-hub lease objects
	peerLeasePrefix string = "clusternet-hub-peer"
)

// Hub defines configuration for clusternet-hub
type Hub struct {
	peerID  string
	options *options.HubServerOptions

	clusternetInformerFactory informers.SharedInformerFactory
	kubeInformerFactory       kubeinformers.SharedInformerFactory
	aggregatorInformerFactory aggregatorinformers.SharedInformerFactory

	kubeClient       *kubernetes.Clientset
	electionClient   *kubernetes.Clientset
	clusternetClient *clusternet.Clientset
	clientBuilder    clientbuilder.ControllerClientBuilder
	recorder         record.EventRecorder

	socketConnection bool
}

// NewHub returns a new Hub.
func NewHub(opts *options.HubServerOptions) (*Hub, error) {
	socketConnection := utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection)
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
	electionClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-hub-election-client"))
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

	hub := &Hub{
		peerID:                    utilrand.String(known.DefaultRandomIDLength),
		options:                   opts,
		kubeClient:                kubeClient,
		electionClient:            electionClient,
		clusternetClient:          clusternetClient,
		clientBuilder:             rootClientBuilder,
		clusternetInformerFactory: clusternetInformerFactory,
		kubeInformerFactory:       kubeInformerFactory,
		aggregatorInformerFactory: aggregatorInformerFactory,
		socketConnection:          socketConnection,
		recorder:                  recorder,
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

	completeConfig := config.Complete()
	server, err := completeConfig.New(
		hub.peerID,
		hub.options.PeerToken,
		hub.options.TunnelLogging,
		hub.socketConnection,
		hub.options.RecommendedOptions.Authentication.RequestHeader.ExtraHeaderPrefixes,
		hub.clusternetInformerFactory,
		hub.aggregatorInformerFactory)
	if err != nil {
		return err
	}

	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}

	// create lease for peer
	peerLease, err := createPeerLease(
		peerInfo{
			ID:       hub.peerID,
			Identity: curIdentity,
			Host:     hub.options.PeerAdvertiseAddress.String(),
			Port:     hub.options.PeerPort,
			Token:    hub.options.PeerToken,
		},
		hub.options.LeaderElection,
		hub.electionClient,
	)
	if err != nil {
		return err
	}

	klog.Infof("starting Clusternet informers ...")
	// Start the informer factories to begin populating the informer caches
	// Start method is non-blocking and runs all registered informers in a dedicated goroutine.
	hub.kubeInformerFactory.Start(ctx.Done())
	hub.clusternetInformerFactory.Start(ctx.Done())
	hub.aggregatorInformerFactory.Start(ctx.Done())
	config.GenericConfig.SharedInformerFactory.Start(ctx.Done())
	// no need to start LoopbackSharedInformerFactory since we don't store anything in this apiserver
	// hub.options.LoopbackSharedInformerFactory.Start(ctx.Done())

	server.GenericAPIServer.AddPostStartHookOrDie("start-clusternet-hub-shadowapis",
		func(postStartHookContext genericapiserver.PostStartHookContext) error {
			if server.GenericAPIServer != nil && utilfeature.DefaultFeatureGate.Enabled(features.ShadowAPI) {
				klog.Infof("install shadow apis...")
				// waits for all started informers' cache got synced
				hub.aggregatorInformerFactory.WaitForCacheSync(postStartHookContext.StopCh)

				crdInformerFactory := crdinformers.NewSharedInformerFactory(
					crdclientset.NewForConfigOrDie(hub.clientBuilder.ConfigOrDie("crd-shared-informers")),
					5*time.Minute,
				)

				ss := shadowapiserver.NewShadowAPIServer(server.GenericAPIServer,
					completeConfig.GenericConfig.MaxRequestBodyBytes,
					completeConfig.GenericConfig.MinRequestTimeout,
					completeConfig.GenericConfig.AdmissionControl,
					hub.kubeClient.RESTClient(),
					hub.clusternetClient,
					hub.clusternetInformerFactory.Apps().V1alpha1().Manifests().Lister(),
					hub.aggregatorInformerFactory.Apiregistration().V1().APIServices().Lister(),
					crdInformerFactory,
					hub.options.ReservedNamespace)

				crdInformerFactory.Start(postStartHookContext.StopCh)
				return ss.InstallShadowAPIGroups(postStartHookContext.StopCh, hub.kubeClient.DiscoveryClient)
			}

			select {
			case <-postStartHookContext.StopCh:
			}

			return nil
		},
	)

	server.GenericAPIServer.AddPostStartHookOrDie("serving-peer-connections",
		func(postStartHookContext genericapiserver.PostStartHookContext) error {
			if !hub.options.LeaderElection.LeaderElect {
				klog.Info("no need to serve peer connections")
				return nil
			}

			serverCert := &hub.options.RecommendedOptions.SecureServing.ServerCert
			certFile := serverCert.CertKey.CertFile
			keyFile := serverCert.CertKey.KeyFile
			if len(certFile) == 0 && len(keyFile) == 0 {
				// use default self signed certs instead
				certFile = path.Join(serverCert.CertDirectory, serverCert.PairName+".crt")
				keyFile = path.Join(serverCert.CertDirectory, serverCert.PairName+".key")
			}

			peerServing := &http.Server{
				Addr:    fmt.Sprintf("%s:%d", hub.options.RecommendedOptions.SecureServing.BindAddress, hub.options.PeerPort),
				Handler: newPeerRouter(server.PeerDialer),
			}
			defer func() {
				// use Close() to close all active socket connections
				err = peerServing.Close()
				if err != nil {
					klog.Fatalf("failed to shutdown peer server: %v", err)
				}
			}()

			go leasegc.NewLeaseGC(
				hub.electionClient,
				hub.options.LeaderElection.LeaseDuration.Duration,
				hub.options.LeaderElection.ResourceNamespace,
				func(lease *coordinationapi.Lease) bool {
					return strings.HasPrefix(lease.Name, peerLeasePrefix)
				},
			).Run(ctx.Done())

			go wait.UntilWithContext(ctx, func(ctx context.Context) {
				peerLease.Run(ctx)
			}, 0)

			go func() {
				klog.Info("starting serving peer connections")
				if err = peerServing.ListenAndServeTLS(certFile, keyFile); err != http.ErrServerClosed {
					klog.Fatalf("failed to serve peer connections: %v", err)
					os.Exit(1)
				}
			}()

			go refreshingPeers(hub.electionClient, hub.peerID, hub.options.LeaderElection.ResourceNamespace,
				server.PeerDialer, postStartHookContext.StopCh)

			select {
			case <-postStartHookContext.StopCh:
			}

			return nil
		},
	)

	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}

func createPeerLease(peer peerInfo, leaderOption componentbaseconfig.LeaderElectionConfiguration,
	clientset kubernetes.Interface) (*leaderelection.LeaderElector, error) {
	peerLeaseName := fmt.Sprintf("%s-%s", peerLeasePrefix, peer.ID)

	peerBytes, err := utils.Marshal(peer)
	if err != nil {
		return nil, err
	}

	leaderElectionConfig := leaderelection.LeaderElectionConfig{
		Lock: &resourcelock.LeaseLock{
			LeaseMeta: metav1.ObjectMeta{
				Name:      peerLeaseName,
				Namespace: leaderOption.ResourceNamespace,
			},
			Client: clientset.CoordinationV1(),
			LockConfig: resourcelock.ResourceLockConfig{
				Identity: string(peerBytes),
			},
		},
		// IMPORTANT: you MUST ensure that any code you have that
		// is protected by the lease must terminate **before**
		// you call cancel. Otherwise, you could have a background
		// loop still running and another process could
		// get elected before your background loop finished, violating
		// the stated goal of the lease.
		ReleaseOnCancel: true,
		LeaseDuration:   leaderOption.LeaseDuration.Duration,
		RenewDeadline:   leaderOption.RenewDeadline.Duration,
		RetryPeriod:     leaderOption.RetryPeriod.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaseCtx context.Context) {
				// // no-op
				<-leaseCtx.Done()
			},
			OnStoppedLeading: func() {
				klog.Errorf("peer lease %s got lost", peerLeaseName)
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if peer.Identity == identity {
					// I just got the lock
					return
				}
				// this should not happen
				klog.Warningf("peer lease %s got unexpected leader %s", peerLeaseName, identity)
			},
		},
	}

	return leaderelection.NewLeaderElector(leaderElectionConfig)
}

func refreshingPeers(clientset kubernetes.Interface, selfID, leaseNamespace string, peerDialer *remotedialer.Server, stopCh <-chan struct{}) {
	leaseInformer := coordinationinformers.NewFilteredLeaseInformer(
		clientset,
		leaseNamespace,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil)

	_, err := leaseInformer.AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			lease := obj.(*coordinationapi.Lease)
			if strings.HasPrefix(lease.Name, peerLeasePrefix) {
				if fmt.Sprintf("%s-%s", peerLeasePrefix, selfID) == lease.Name {
					return false
				}
				return true
			}
			return false
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				addPeer(peerDialer, obj.(*coordinationapi.Lease))
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldLease := oldObj.(*coordinationapi.Lease)
				newLease := newObj.(*coordinationapi.Lease)

				if oldLease.Spec.HolderIdentity != nil && newLease.Spec.HolderIdentity != nil &&
					*oldLease.Spec.HolderIdentity == *newLease.Spec.HolderIdentity {
					return
				}

				if newLease.Spec.HolderIdentity != nil {
					addPeer(peerDialer, newLease)
					return
				}

				// lease lost
				removePeer(peerDialer, newLease)
			},
			DeleteFunc: func(obj interface{}) {
				removePeer(peerDialer, obj.(*coordinationapi.Lease))
			},
		},
	})
	if err != nil {
		klog.Fatalf("failed to add event handler for hub peer lease: %v", err)
		return
	}

	klog.Infof("refreshing peer connections ...")
	leaseInformer.Run(stopCh)
}

func newPeerRouter(handler http.Handler) *mux.Router {
	router := mux.NewRouter()
	router.Handle("/connect", handler)
	return router
}

func addPeer(peerDialer *remotedialer.Server, lease *coordinationapi.Lease) {
	pi, err := getPeerInfo(lease)
	if err != nil {
		// TODO
		return
	}

	if pi == nil {
		return
	}

	url := fmt.Sprintf("wss://%s/connect", pi.Host)
	if pi.Port > 0 {
		url = fmt.Sprintf("wss://%s:%d/connect", pi.Host, pi.Port)
	}
	peerDialer.AddPeer(url, pi.ID, pi.Token)
	klog.Infof("successfully add peer %s with url %s", pi.ID, url)
}

func removePeer(peerDialer *remotedialer.Server, lease *coordinationapi.Lease) {
	peerID := strings.TrimPrefix(lease.Name, peerLeasePrefix+"-")
	peerDialer.RemovePeer(peerID)
	klog.Infof("successfully remove peer %s", peerID)
}

func getPeerInfo(lease *coordinationapi.Lease) (*peerInfo, error) {
	if !strings.HasPrefix(lease.Name, peerLeasePrefix) {
		return nil, nil
	}

	if lease.Spec.HolderIdentity == nil {
		return &peerInfo{ID: strings.TrimPrefix(lease.Name, peerLeasePrefix+"-")}, nil
	}

	pi := &peerInfo{}
	err := utils.Unmarshal([]byte(*lease.Spec.HolderIdentity), pi)
	return pi, err
}

type peerInfo struct {
	Identity string `json:"identity,omitempty"`
	ID       string `json:"id,omitempty"`
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Token    string `json:"token,omitempty"`
}
