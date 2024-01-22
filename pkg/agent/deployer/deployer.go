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

package deployer

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/agent/deployer/generic"
	"github.com/clusternet/clusternet/pkg/agent/deployer/helm"
	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

type Deployer struct {
	// syncMode indicates current sync mode
	syncMode clusterapi.ClusterSyncMode
	// whether AppPusher feature gate is enabled
	appPusherEnabled bool

	childAPIServerURL string
	// systemNamespace specifies the default namespace to look up credentials
	// default to be "clusternet-system"
	systemNamespace string

	saTokenAutoGen bool
}

func NewDeployer(syncMode, childAPIServerURL, systemNamespace string, saTokenAutoGen bool) *Deployer {
	return &Deployer{
		syncMode:          clusterapi.ClusterSyncMode(syncMode),
		appPusherEnabled:  utilfeature.DefaultFeatureGate.Enabled(features.AppPusher),
		childAPIServerURL: childAPIServerURL,
		systemNamespace:   systemNamespace,
		saTokenAutoGen:    saTokenAutoGen,
	}
}

func (d *Deployer) Run(
	ctx context.Context,
	parentDedicatedKubeConfig *rest.Config,
	childKubeClientSet kubernetes.Interface,
	dedicatedNamespace *string,
	clusterID *types.UID, workers int,
	kubeQPS float32,
	kubeBurst int32,
) error {
	klog.Infof("starting deployer ...")

	// in case the dedicated kubeconfig get changed when leader election gets lost,
	// initialize the client when Run() is called
	parentClientSet := kubernetes.NewForConfigOrDie(parentDedicatedKubeConfig)

	if dedicatedNamespace == nil {
		klog.Error("unexpected nil dedicatedNamespace")
		// in case a race condition here
		os.Exit(1)
		return nil
	}

	// make sure deployer gets initialized before we go next
	appDeployerSecret := utils.GetDeployerCredentials(ctx, childKubeClientSet, d.systemNamespace, d.saTokenAutoGen)

	// creating credentials to parent cluster is also required in the pull mode,
	// because the cleaning of redundant objects in the description is performed by clusternet-controller-manager
	klog.V(4).Infof("initializing deployer with sync mode %s", d.syncMode)
	createDeployerCredentialsToParentCluster(ctx, parentClientSet, string(*clusterID), *dedicatedNamespace, d.childAPIServerURL, appDeployerSecret)

	// setup broadcaster and event recorder in parent cluster
	broadcaster := record.NewBroadcaster()
	klog.Infof("sending events to parent apiserver")
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubernetes.NewForConfigOrDie(parentDedicatedKubeConfig).CoreV1().Events(""),
	})
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	parentRecorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-agent"})

	// create clientset and informerFactory
	clusternetclient := clusternet.NewForConfigOrDie(parentDedicatedKubeConfig)
	clusternetInformerFactory := informers.NewSharedInformerFactoryWithOptions(clusternetclient,
		known.DefaultResync, informers.WithNamespace(*dedicatedNamespace))
	// add informers for HelmReleases and Descriptions
	clusternetInformerFactory.Apps().V1alpha1().HelmReleases().Informer()
	clusternetInformerFactory.Apps().V1alpha1().Descriptions().Informer()
	clusternetInformerFactory.Start(ctx.Done())

	appDeployerConfig, err := utils.GenerateKubeConfigFromToken(
		d.childAPIServerURL,
		string(appDeployerSecret.Data[corev1.ServiceAccountTokenKey]),
		appDeployerSecret.Data[corev1.ServiceAccountRootCAKey],
	)
	if err != nil {
		return err
	}
	appDeployerConfig.QPS = kubeQPS
	appDeployerConfig.Burst = int(kubeBurst)
	genericDeployer, err := generic.NewDeployer(d.syncMode, d.appPusherEnabled, appDeployerConfig,
		clusternetclient, clusternetInformerFactory, parentRecorder)
	if err != nil {
		return err
	}

	deployCtx, err := utils.NewDeployContext(
		utils.CreateKubeConfigWithToken(
			d.childAPIServerURL,
			string(appDeployerSecret.Data[corev1.ServiceAccountTokenKey]),
			appDeployerSecret.Data[corev1.ServiceAccountRootCAKey],
		),
		kubeQPS,
		kubeBurst,
	)
	if err != nil {
		return err
	}
	helmDeployer, err := helm.NewDeployer(
		d.syncMode,
		d.appPusherEnabled,
		parentClientSet,
		clusternetclient,
		deployCtx,
		clusternetInformerFactory,
		parentRecorder,
	)
	if err != nil {
		return err
	}
	go genericDeployer.Run(workers, ctx)
	go helmDeployer.Run(workers, ctx)

	<-ctx.Done()
	return nil
}

func createDeployerCredentialsToParentCluster(ctx context.Context, parentClientSet *kubernetes.Clientset,
	clusterID, dedicatedNamespace, childAPIServerURL string, secretInChildCluster *corev1.Secret) {
	// create child cluster credentials to parent cluster for app deployer
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      known.ChildClusterSecretName,
			Namespace: dedicatedNamespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel:      known.ClusternetAgentName,
				known.ClusterIDLabel:            clusterID,
				known.ClusterBootstrappingLabel: known.CredentialsAuto,
			},
			Annotations: map[string]string{
				known.AutoUpdateAnnotation: "true",
			},
			Finalizers: []string{
				known.AppFinalizer,
			},
		},
		Data: map[string][]byte{
			corev1.ServiceAccountRootCAKey: secretInChildCluster.Data[corev1.ServiceAccountRootCAKey],
			corev1.ServiceAccountTokenKey:  secretInChildCluster.Data[corev1.ServiceAccountTokenKey],
			ServiceAccountNameKey:          []byte(secretInChildCluster.Annotations[corev1.ServiceAccountNameKey]),
			ServiceAccountUIDKey:           []byte(secretInChildCluster.Annotations[corev1.ServiceAccountUIDKey]),
			known.ClusterAPIServerURLKey:   []byte(childAPIServerURL),
		},
	}

	localCtx, cancel := context.WithCancel(ctx)
	wait.JitterUntilWithContext(localCtx, func(ctx context.Context) {
		_, err := parentClientSet.CoreV1().Secrets(dedicatedNamespace).Create(ctx, secret, metav1.CreateOptions{})
		if err == nil {
			klog.V(5).Infof("successfully create deployer credentials %s in parent cluster", klog.KObj(secret))
			cancel()
			return
		} else {
			if apierrors.IsAlreadyExists(err) {
				klog.V(5).Infof("found existed Secret %s in parent cluster, will try to update it", klog.KObj(secret))

				// try to auto update existing object
				var sct *corev1.Secret
				sct, err = parentClientSet.CoreV1().Secrets(dedicatedNamespace).Get(ctx, secret.Name, metav1.GetOptions{})
				if err != nil {
					klog.ErrorDepth(5, fmt.Sprintf("failed to get Secret %s in parent cluster: %v, will retry",
						klog.KObj(secret), err))
					return
				}
				if autoUpdate, ok := sct.Annotations[known.AutoUpdateAnnotation]; ok && autoUpdate == "true" {
					_, err = parentClientSet.CoreV1().Secrets(dedicatedNamespace).Update(ctx, secret, metav1.UpdateOptions{})
					if err == nil {
						// success on the updating
						cancel()
						return
					}
					klog.ErrorDepth(5, fmt.Sprintf("failed to update Secret %s in parent cluster: %v, will retry",
						klog.KObj(secret), err))
					return
				}
			}
			klog.ErrorDepth(5, fmt.Sprintf("failed to create Secret %s: %v, will retry", klog.KObj(secret), err))
		}
	}, known.DefaultRetryPeriod, 0.4, true)
}
