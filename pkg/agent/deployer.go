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

package agent

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/known"
)

type Deployer struct {
	SyncMode clusterapi.ClusterSyncMode
	// clientset for child cluster
	childKubeClientSet *kubernetes.Clientset
	childAPIServerURL  string
	// whether AppPusher feature gate is enabled
	appPusherEnabled bool
}

func NewDeployer(syncMode, childAPIServerURL string, childKubeClientSet *kubernetes.Clientset) *Deployer {
	return &Deployer{
		SyncMode:           clusterapi.ClusterSyncMode(syncMode),
		childAPIServerURL:  childAPIServerURL,
		childKubeClientSet: childKubeClientSet,
		appPusherEnabled:   utilfeature.DefaultFeatureGate.Enabled(features.AppPusher),
	}
}

func (d *Deployer) Run(ctx context.Context, parentDedicatedKubeConfig *rest.Config, secret *corev1.Secret, clusterID *types.UID) {
	klog.Infof("starting deployer ...")

	// in case the dedicated kubeconfig get changed when leader election gets lost,
	// initialize the client when Run() is called
	parentClientSet := kubernetes.NewForConfigOrDie(parentDedicatedKubeConfig)

	if secret == nil {
		klog.Error("unexpected nil secret")
		// in case a race condition here
		os.Exit(1)
		return
	}

	dedicatedNamespace := string(secret.Data[corev1.ServiceAccountNamespaceKey])
	// make sure deployer gets initialized before we go next
	d.getInitialized(ctx, dedicatedNamespace, parentClientSet, clusterID)

	// TODO: add puller ops
}

func (d *Deployer) getInitialized(ctx context.Context, dedicatedNamespace string, parentClientSet *kubernetes.Clientset, clusterID *types.UID) {
	if d.appPusherEnabled && d.SyncMode != clusterapi.Pull {
		klog.V(4).Infof("initializing deployer with sync mode %s", d.SyncMode)

		secret := d.getDeployerCredentials(ctx)
		createDeployerCredentialsToParentCluster(ctx, parentClientSet, string(*clusterID), dedicatedNamespace, d.childAPIServerURL, secret)
	}
}

func (d *Deployer) getDeployerCredentials(ctx context.Context) *corev1.Secret {
	var secret *corev1.Secret
	localCtx, cancel := context.WithCancel(ctx)

	klog.V(4).Infof("get ServiceAccount %s/%s", ClusternetSystemNamespace, ClusternetAppSA)
	wait.JitterUntil(func() {
		sa, err := d.childKubeClientSet.CoreV1().ServiceAccounts(ClusternetSystemNamespace).Get(localCtx, ClusternetAppSA, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Errorf("failed to get ServiceAccount %s/%s: %v", ClusternetSystemNamespace, ClusternetAppSA, err))
			return
		}

		if len(sa.Secrets) == 0 {
			klog.ErrorDepth(5, fmt.Errorf("no secrets found in ServiceAccount %s/%s", ClusternetSystemNamespace, ClusternetAppSA))
			return
		}

		secret, err = d.childKubeClientSet.CoreV1().Secrets(ClusternetSystemNamespace).Get(localCtx, sa.Secrets[0].Name, metav1.GetOptions{})
		if err != nil {
			klog.ErrorDepth(5, fmt.Errorf("failed to get Secret %s/%s: %v", ClusternetSystemNamespace, sa.Secrets[0].Name, err))
			return
		}

		cancel()
		return
	}, DefaultRetryPeriod, 0.4, true, localCtx.Done())

	klog.V(4).Info("successfully get credentials populated for deployer")
	return secret
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
				known.AutoUpdateAnnotationKey: "true",
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
	wait.JitterUntil(func() {
		_, err := parentClientSet.CoreV1().Secrets(dedicatedNamespace).Create(localCtx, secret, metav1.CreateOptions{})
		if err == nil {
			klog.V(5).Infof("successfully create deployer credentials %s in parent cluster", klog.KObj(secret))
			cancel()
			return
		} else {
			if apierrors.IsAlreadyExists(err) {
				klog.V(5).Infof("found existed Secret %s in parent cluster, will try to update it", klog.KObj(secret))

				// try to auto update existing object
				sct, err := parentClientSet.CoreV1().Secrets(dedicatedNamespace).Get(localCtx, secret.Name, metav1.GetOptions{})
				if err != nil {
					klog.ErrorDepth(5, fmt.Sprintf("failed to get Secret %s in parent cluster: %v, will retry",
						klog.KObj(secret), err))
					return
				}
				if autoUpdate, ok := sct.Annotations[known.AutoUpdateAnnotationKey]; ok && autoUpdate == "true" {
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
	}, DefaultRetryPeriod, 0.4, true, localCtx.Done())
}
