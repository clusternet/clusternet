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
	"errors"
	"os"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterstatus"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

type Manager struct {
	// statusReportFrequency is the frequency at which the agent reports current cluster's status
	statusReportFrequency metav1.Duration

	clusterStatusController *clusterstatus.Controller

	managedCluster *clusterapi.ManagedCluster
}

func NewStatusManager(ctx context.Context, apiserverURL string, regOpts *ClusterRegistrationOptions, kubeClient kubernetes.Interface) *Manager {
	retryCtx, retryCancel := context.WithTimeout(ctx, known.DefaultRetryPeriod)
	defer retryCancel()

	secret := utils.GetDeployerCredentials(retryCtx, kubeClient)
	if secret != nil {
		clusterStatusKubeConfig, err := utils.GenerateKubeConfigFromToken(apiserverURL,
			string(secret.Data[corev1.ServiceAccountTokenKey]), secret.Data[corev1.ServiceAccountRootCAKey], 2)
		if err == nil {
			kubeClient = kubernetes.NewForConfigOrDie(clusterStatusKubeConfig)
		}
	}

	return &Manager{
		statusReportFrequency: regOpts.ClusterStatusReportFrequency,
		clusterStatusController: clusterstatus.NewController(ctx, apiserverURL,
			kubeClient, regOpts.ClusterStatusCollectFrequency, regOpts.ClusterStatusReportFrequency),
	}
}

func (mgr *Manager) Run(ctx context.Context, parentDedicatedKubeConfig *rest.Config, dedicatedNamespace *string, clusterID *types.UID) {
	klog.Infof("starting status manager to report heartbeats...")

	go mgr.clusterStatusController.Run(ctx)

	// in case the dedicated kubeconfig get changed when leader election gets lost,
	// initialize the client when Run() is called
	client := clusternetclientset.NewForConfigOrDie(parentDedicatedKubeConfig)

	wait.UntilWithContext(ctx, func(ctx context.Context) {
		if dedicatedNamespace == nil {
			klog.Error("unexpected nil dedicatedNamespace")
			// in case a race condition here
			os.Exit(1)
			return
		}
		if clusterID == nil {
			klog.Error("unexpected nil clusterID")
			// in case a race condition here
			os.Exit(1)
			return
		}
		mgr.updateClusterStatus(ctx, *dedicatedNamespace, string(*clusterID), client, retry.DefaultBackoff)
	}, mgr.statusReportFrequency.Duration)
}

func (mgr *Manager) updateClusterStatus(ctx context.Context, namespace, clusterID string, client clusternetclientset.Interface, backoff wait.Backoff) {
	if mgr.managedCluster == nil {
		managedClusters, err := client.ClustersV1beta1().ManagedClusters(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				known.ClusterIDLabel: clusterID,
			}).String(),
		})
		if err != nil {
			klog.Errorf("failed to list ManagedCluster in namespace %s: %v", namespace, err)
			return
		}

		if len(managedClusters.Items) > 0 {
			if len(managedClusters.Items) > 1 {
				klog.Warningf("found multiple ManagedCluster for cluster %s in namespace %s !!!", clusterID, namespace)
			}
			mgr.managedCluster = new(clusterapi.ManagedCluster)
			*mgr.managedCluster = managedClusters.Items[0]
		} else {
			klog.Warningf("unable to get a matching ManagedCluster for cluster %s, will retry later", clusterID)
			return
		}
	}

	// in case the network is not stable, retry with backoff
	var lastError error
	var mcls *clusterapi.ManagedCluster
	err := wait.ExponentialBackoffWithContext(ctx, backoff, func() (bool, error) {
		status := mgr.clusterStatusController.GetClusterStatus()
		if status == nil {
			lastError = errors.New("cluster status is not ready, will retry later")
			return false, nil
		}

		mgr.managedCluster.Status = *status
		mcls, lastError = client.ClustersV1beta1().ManagedClusters(namespace).UpdateStatus(ctx, mgr.managedCluster, metav1.UpdateOptions{})
		if lastError == nil {
			mgr.managedCluster = mcls
			return true, nil
		}
		if apierrors.IsConflict(lastError) {
			mcls, lastError = client.ClustersV1beta1().ManagedClusters(namespace).Get(ctx, mgr.managedCluster.Name, metav1.GetOptions{})
			if lastError == nil {
				mgr.managedCluster = mcls
			}
		}
		return false, nil
	})
	if err != nil {
		klog.WarningDepth(2, "failed to update status of ManagedCluster: %v", lastError)
	}
}
