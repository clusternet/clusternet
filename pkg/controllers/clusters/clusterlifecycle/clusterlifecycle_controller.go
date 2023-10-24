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

package clusterlifecycle

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusterinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/known"
	taintutils "github.com/clusternet/clusternet/pkg/utils/taints"
)

const (
	DefaultClusterMonitorGracePeriod = 9 * time.Minute

	// ClusterNotReadyThreshold indicate the threshold for cluster not ready period
	ClusterNotReadyThreshold = 3
)

var (
	// map {ConditionType: {ConditionStatus: TaintKey}}
	// represents which ConditionType under which ConditionStatus should be
	// tainted with which TaintKey
	// for certain ConditionType, there are multiple {ConditionStatus,TaintKey} pairs
	clusterConditionToTaintKeyStatusMap = map[string]map[metav1.ConditionStatus]string{
		clusterapi.ClusterReady: {
			metav1.ConditionFalse:   known.TaintClusterUnschedulable,
			metav1.ConditionUnknown: known.TaintClusterUnschedulable,
		},
	}

	taintKeyToNodeConditionMap = map[string]string{
		known.TaintClusterUnschedulable: clusterapi.ClusterReady,
	}
)

// Controller is a controller that manages cluster's lifecycle
type Controller struct {
	clusternetClient clusternetclientset.Interface
	recorder         record.EventRecorder
	yachtController  *yacht.Controller
	clusterInformer  clusterinformers.ManagedClusterInformer
}

func NewController(clusternetClient clusternetclientset.Interface,
	clusterInformer clusterinformers.ManagedClusterInformer, recorder record.EventRecorder) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		recorder:         recorder,
		clusterInformer:  clusterInformer,
	}

	yachtController := yacht.NewController("cluster-lifecycle").
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: status change
			if oldObj != nil && newObj != nil {
				oldMcls := oldObj.(*clusterapi.ManagedCluster)
				newMcls := newObj.(*clusterapi.ManagedCluster)
				if newMcls.DeletionTimestamp != nil {
					return true, nil
				}
				// Decide whether discovery has reported a status change.
				if equality.Semantic.DeepEqual(oldMcls.Status, newMcls.Status) {
					klog.V(4).Infof("no updates on the status of ManagedCluster %s, skipping syncing", klog.KObj(oldMcls))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// add event handler for ManagedCluster on the addition/update
	clusterEventHandler := yachtController.DefaultResourceEventHandlerFuncs()
	clusterEventHandler.DeleteFunc = nil // only care cluster add/update
	_, err := clusterInformer.Informer().AddEventHandler(clusterEventHandler)
	if err != nil {
		return nil, err
	}
	c.yachtController = yachtController
	return c, nil
}

func (c *Controller) Run(workers int, ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting cluster lifecycle controller")
	defer klog.Infof("Shutting down cluster lifecycle controller")

	c.yachtController.WithWorkers(workers).Run(ctx)
}

func (c *Controller) handle(key interface{}) (requeueAfter *time.Duration, err error) {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing ManagedCluster %q", key)
	// Get the ManagedCluster resource with this name
	mcls, err := c.clusterInformer.Lister().ManagedClusters(ns).Get(name)
	// The ManagedCluster resource may no longer exist, in which case we stop processing.
	if apierrors.IsNotFound(err) {
		klog.V(2).Infof("ManagedCluster %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mcls.DeletionTimestamp != nil {
		klog.V(2).Infof("ManagedCluster %q is being deleted. Skip it", key)
		return nil, nil
	}

	updatedMcls, err := c.updateClusterCondition(context.TODO(), mcls.DeepCopy())
	if err != nil {
		msg := fmt.Sprintf("failed to update conditions of ManagedCluster status: %v", err)
		klog.WarningDepth(2, msg)
		c.recorder.Event(mcls, corev1.EventTypeWarning, "FailedUpdatingClusterStatus", msg)
		return nil, err
	}

	err = c.updateClusterTaints(context.TODO(), updatedMcls)
	if err != nil {
		return nil, err
	}

	// requeueAfter is the duration that we consider a cluster to be Unknown
	// the duration is the same as the grace period for cluster monitoring
	return getClusterMonitorGracePeriod(mcls.Status), nil
}

func (c *Controller) updateClusterCondition(ctx context.Context, cluster *clusterapi.ManagedCluster) (*clusterapi.ManagedCluster, error) {
	currentReadyCondition := GetClusterCondition(&cluster.Status, clusterapi.ClusterReady)
	observedReadyCondition := generateClusterReadyCondition(cluster)
	if observedReadyCondition == nil || equality.Semantic.DeepEqual(currentReadyCondition, observedReadyCondition) {
		return cluster, nil
	}

	// update Readyz,Livez,Healthz to false when cluster status is not ready
	if observedReadyCondition.Status != metav1.ConditionTrue {
		cluster.Status.Readyz = false
		cluster.Status.Livez = false
		cluster.Status.Healthz = false
	}

	// TODO: multiple cluster conditions
	cluster.Status.Conditions = []metav1.Condition{*observedReadyCondition}
	return c.clusternetClient.ClustersV1beta1().ManagedClusters(cluster.Namespace).UpdateStatus(ctx,
		cluster, metav1.UpdateOptions{})
}

func (c *Controller) updateClusterTaints(ctx context.Context, cluster *clusterapi.ManagedCluster) error {
	// Map cluster's condition to Taints.
	var taints []corev1.Taint
	for _, condition := range cluster.Status.Conditions {
		if taintMap, found := clusterConditionToTaintKeyStatusMap[condition.Type]; found {
			if taintKey, found2 := taintMap[condition.Status]; found2 {
				taints = append(taints, corev1.Taint{
					Key:    taintKey,
					Effect: corev1.TaintEffectNoSchedule,
				})
			}
		}
	}

	// Get exist taints of cluster.
	clusterTaints := taintutils.TaintSetFilter(cluster.Spec.Taints, func(t *corev1.Taint) bool {
		// only NoSchedule taints are candidates to be compared with "taints" later
		if t.Effect != corev1.TaintEffectNoSchedule {
			return false
		}
		// Find unschedulable taint of cluster.
		if t.Key == known.TaintClusterUnschedulable {
			return true
		}
		// Find cluster condition taints of cluster.
		_, found := taintKeyToNodeConditionMap[t.Key]
		return found
	})
	taintsToAdd, taintsToDel := taintutils.TaintSetDiff(taints, clusterTaints)
	// If nothing to add or delete, return true directly.
	if len(taintsToAdd) == 0 && len(taintsToDel) == 0 {
		return nil
	}

	// patch taints
	return patchClusterTaints(ctx, c.clusternetClient, cluster, taintsToAdd, taintsToDel)
}

func patchClusterTaints(ctx context.Context, client clusternetclientset.Interface, cluster *clusterapi.ManagedCluster,
	taintsToAdd, taintsToRemove []*corev1.Taint) error {
	var err error
	var newCluster *clusterapi.ManagedCluster = cluster
	for _, taint := range taintsToRemove {
		newCluster, _, err = taintutils.RemoveTaint(cluster, taint)
		if err != nil {
			return err
		}
	}
	for _, taint := range taintsToAdd {
		newCluster, _, err = taintutils.AddOrUpdateTaint(newCluster, taint)
		if err != nil {
			return err
		}
	}

	// patch ManagedCluster
	oldData, err := json.Marshal(cluster)
	if err != nil {
		return err
	}
	newData, err := json.Marshal(newCluster)
	if err != nil {
		return err
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, clusterapi.ManagedCluster{})
	if err != nil {
		return err
	}
	if len(patchBytes) == 0 {
		return nil
	}
	_, err = client.ClustersV1beta1().ManagedClusters(cluster.Namespace).Patch(ctx, cluster.Name, types.MergePatchType,
		patchBytes, metav1.PatchOptions{})
	return err
}

// generateClusterReadyCondition return cluster unknown condition based on cluster status and grace period.
func generateClusterReadyCondition(cluster *clusterapi.ManagedCluster) *metav1.Condition {
	lastObservedTime := cluster.Status.LastObservedTime
	probeTimestamp := lastObservedTime
	if lastObservedTime.IsZero() {
		// If lastObservedTime is zero means Clusternet agent never posted cluster status.
		// We treat this cluster to be a new cluster and use cluster.CreationTimestamp to be probeTimestamp.
		probeTimestamp = cluster.CreationTimestamp
	}

	if !metav1.Now().After(probeTimestamp.Add(*getClusterMonitorGracePeriod(cluster.Status))) {
		return nil
	}

	if lastObservedTime.IsZero() {
		// Clusternet agent never posted cluster status and reach grace period
		return &metav1.Condition{
			Type:               clusterapi.ClusterReady,
			Status:             metav1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             "ClusterStatusNeverUpdated",
			Message:            "Clusternet agent never posted cluster status.",
		}
	}
	// Clusternet agent is stopping posting cluster status
	return &metav1.Condition{
		Type:               clusterapi.ClusterReady,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             "ClusterStatusUnknown",
		Message:            "Clusternet agent stopped posting cluster status.",
	}
}

// GetClusterCondition extracts the provided condition from the given status and returns that.
func GetClusterCondition(status *clusterapi.ManagedClusterStatus, conditionType string) *metav1.Condition {
	if status == nil {
		return nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return &status.Conditions[i]
		}
	}
	return nil
}

// getClusterMonitorGracePeriod calculate grace period for cluster monitoring, use DefaultClusterMonitorGracePeriod if HeartbeatFrequencySeconds is undefined.
func getClusterMonitorGracePeriod(status clusterapi.ManagedClusterStatus) *time.Duration {
	gracePeriod := DefaultClusterMonitorGracePeriod
	if status.HeartbeatFrequencySeconds != nil {
		gracePeriod = time.Second * time.Duration(*status.HeartbeatFrequencySeconds) * ClusterNotReadyThreshold
	}
	return &gracePeriod
}
