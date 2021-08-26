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
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/controllers/clusters/clusterstatus"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
)

type Manager struct {
	// statusReportFrequency is the frequency at which the agent reports current cluster's status
	statusReportFrequency metav1.Duration

	clusterStatusController *clusterstatus.Controller

	managedCluster *clusterapi.ManagedCluster
}

func NewStatusManager(ctx context.Context, apiserverURL, parentAPIServerURL string, kubeClient kubernetes.Interface, statusCollectFrequency metav1.Duration, statusReportFrequency metav1.Duration) *Manager {
	return &Manager{
		statusReportFrequency:   statusReportFrequency,
		clusterStatusController: clusterstatus.NewController(ctx, apiserverURL, parentAPIServerURL, kubeClient, statusCollectFrequency),
	}
}

func (mgr *Manager) Run(ctx context.Context, parentDedicatedKubeConfig *rest.Config, secret *corev1.Secret) {
	klog.Infof("starting status manager to report heartbeats...")

	go mgr.clusterStatusController.Run(ctx)

	// in case the dedicated kubeconfig get changed when leader election gets lost,
	// initialize the client when Run() is called
	client := clusternetClientSet.NewForConfigOrDie(parentDedicatedKubeConfig)
	wait.Until(func() {
		if secret == nil {
			klog.Error("unexpected nil secret")
			// in case a race condition here
			os.Exit(1)
			return
		}

		namespace, ok := secret.Data[corev1.ServiceAccountNamespaceKey]
		if !ok {
			klog.Errorf("cannot find secret data 'namespace' in secret %s", klog.KObj(secret))
			return
		}

		clusterID, ok := secret.Labels[known.ClusterIDLabel]
		if !ok {
			klog.Errorf("cannot find label %s in secret %s", known.ClusterIDLabel, klog.KObj(secret))
			return
		}

		mgr.updateClusterStatus(ctx,
			string(namespace),
			clusterID,
			client,
			wait.Backoff{
				Steps:    4,
				Duration: 500 * time.Millisecond,
				Factor:   5.0,
				Jitter:   0.1,
			})
	}, mgr.statusReportFrequency.Duration, ctx.Done())
}

func (mgr *Manager) updateClusterStatus(ctx context.Context, namespace, clusterID string, client clusternetClientSet.Interface, backoff wait.Backoff) {
	if mgr.managedCluster == nil {
		managedClusters, err := client.ClustersV1beta1().ManagedClusters(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(labels.Set{
				known.ClusterIDLabel: string(clusterID),
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
	wait.ExponentialBackoffWithContext(ctx, backoff, func() (done bool, err error) {
		status := mgr.clusterStatusController.GetClusterStatus()
		if status == nil {
			klog.Warningf("cluster status is not ready, will retry later")
			return false, nil
		}

		mgr.managedCluster.Status = *status
		mc, err := client.ClustersV1beta1().ManagedClusters(namespace).UpdateStatus(ctx, mgr.managedCluster, metav1.UpdateOptions{})
		if err != nil {
			if apierrors.IsConflict(err) {
				latestMC, err := client.ClustersV1beta1().ManagedClusters(namespace).Get(ctx, mgr.managedCluster.Name, metav1.GetOptions{})
				if err != nil {
					klog.Errorf("failed to get latest ManagedCluster %s: %v", klog.KObj(mgr.managedCluster), err)
					return false, nil
				}
				mgr.managedCluster = latestMC
				return false, nil
			}

			klog.Errorf("failed to update status of ManagedCluster %s: %v", klog.KObj(mgr.managedCluster), err)
			return false, nil
		}
		mgr.managedCluster = mc
		return true, nil
	})
}
