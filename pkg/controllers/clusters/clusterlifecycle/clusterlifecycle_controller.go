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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	clusterinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
)

const (
	DefaultClusterMonitorGracePeriod = 9 * time.Minute

	// ClusterNotReadyThreshold indicate the threshold for cluster not ready period
	ClusterNotReadyThreshold = 3
)

// Controller is a controller that manages cluster's lifecycle
type Controller struct {
	clusternetClient clusternetClientSet.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	clusterLister clusterlisters.ManagedClusterLister
	clusterSynced cache.InformerSynced

	recorder record.EventRecorder
}

func NewController(clusternetClient clusternetClientSet.Interface,
	clusterInformer clusterinformers.ManagedClusterInformer, recorder record.EventRecorder) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "ManagedCluster"),
		clusterLister:    clusterInformer.Lister(),
		clusterSynced:    clusterInformer.Informer().HasSynced,
		recorder:         recorder,
	}

	_, err := clusterInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addCluster,
		UpdateFunc: c.updateCluster,
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting cluster lifecycle controller")
	defer klog.Infof("Shutting down cluster lifecycle controller")

	// Wait for the caches to be synced before starting monitor lifecycle
	if !cache.WaitForNamedCacheSync("cluster-lifecycle-controller", stopCh, c.clusterSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process ManagedCluster resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addCluster(obj interface{}) {
	mcls := obj.(*clusterapi.ManagedCluster)
	klog.V(4).Infof("adding ManagedCluster %q", klog.KObj(mcls))
	c.enqueue(mcls)
}

func (c *Controller) updateCluster(old, cur interface{}) {
	oldMcls := old.(*clusterapi.ManagedCluster)
	newMcls := cur.(*clusterapi.ManagedCluster)

	if newMcls.DeletionTimestamp != nil {
		return
	}

	// Decide whether discovery has reported a status change.
	if equality.Semantic.DeepEqual(oldMcls.Status, newMcls.Status) {
		klog.V(4).Infof("no updates on the status of ManagedCluster %s, skipping syncing", klog.KObj(oldMcls))
		return
	}

	klog.V(5).Infof("updating ManagedCluster %q", klog.KObj(oldMcls))
	c.enqueue(newMcls)
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// ManagedCluster resource to be synced.
		requeueAfter, err := c.syncHandler(key)
		switch {
		case err != nil:
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		case requeueAfter > 0:
			// Put the item back on the workqueue with delay.
			c.workqueue.Forget(obj)
			c.workqueue.AddAfter(obj, requeueAfter)
		default:
			c.workqueue.Forget(obj)
		}
		klog.Infof("successfully synced ManagedCluster %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the ManagedCluster resource
// with the current status of the resource.
// Duration <=0 means don't need to requeue this obj.
func (c *Controller) syncHandler(key string) (time.Duration, error) {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return time.Duration(0), nil
	}

	klog.V(4).Infof("start processing ManagedCluster %q", key)
	// Get the ManagedCluster resource with this name
	mcls, err := c.clusterLister.ManagedClusters(ns).Get(name)
	// The ManagedCluster resource may no longer exist, in which case we stop processing.
	if apierrors.IsNotFound(err) {
		klog.V(2).Infof("ManagedCluster %q has been deleted", key)
		return time.Duration(0), nil
	}
	if err != nil {
		return time.Duration(0), err
	}

	err = c.updateClusterCondition(context.TODO(), mcls.DeepCopy())
	if err != nil {
		return time.Duration(0), err
	}
	// requeueAfter is the duration that we consider a cluster to be Unknown
	// the duration is the same as the grace period for cluster monitoring
	return getClusterMonitorGracePeriod(mcls.Status), nil
}

// enqueue takes a ManagedCluster resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than ManagedCluster.
func (c *Controller) enqueue(cluster *clusterapi.ManagedCluster) {
	key, err := cache.MetaNamespaceKeyFunc(cluster)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) updateClusterCondition(ctx context.Context, cluster *clusterapi.ManagedCluster) error {
	currentReadyCondition := GetClusterCondition(&cluster.Status, clusterapi.ClusterReady)
	observedReadyCondition := c.generateClusterReadyCondition(cluster)
	if observedReadyCondition == nil || equality.Semantic.DeepEqual(currentReadyCondition, observedReadyCondition) {
		return nil
	}

	// update Readyz,Livez,Healthz to false when cluster status is unknown
	if observedReadyCondition.Status == metav1.ConditionUnknown {
		cluster.Status.Readyz = false
		cluster.Status.Livez = false
		cluster.Status.Healthz = false
	}

	// TODO: multiple cluster conditions
	cluster.Status.Conditions = []metav1.Condition{*observedReadyCondition}
	_, err := c.clusternetClient.ClustersV1beta1().ManagedClusters(cluster.Namespace).UpdateStatus(ctx, cluster, metav1.UpdateOptions{})
	if err != nil {
		msg := fmt.Sprintf("failed to update conditions of ManagedCluster status: %v", err)
		klog.WarningDepth(2, msg)
		c.recorder.Event(cluster, corev1.EventTypeWarning, "FailedUpdatingClusterStatus", msg)
		return err
	}

	return nil
}

// generateClusterReadyCondition return cluster unknown condition based on cluster status and grace period.
func (c *Controller) generateClusterReadyCondition(cluster *clusterapi.ManagedCluster) *metav1.Condition {
	lastObservedTime := cluster.Status.LastObservedTime
	probeTimestamp := lastObservedTime
	if lastObservedTime.IsZero() {
		// If lastObservedTime is zero means Clusternet agent never posted cluster status.
		// We treat this cluster to be a new cluster and use cluster.CreationTimestamp to be probeTimestamp.
		probeTimestamp = cluster.CreationTimestamp
	}

	if !metav1.Now().After(probeTimestamp.Add(getClusterMonitorGracePeriod(cluster.Status))) {
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
func getClusterMonitorGracePeriod(status clusterapi.ManagedClusterStatus) time.Duration {
	gracePeriod := DefaultClusterMonitorGracePeriod
	if status.HeartbeatFrequencySeconds != nil {
		gracePeriod = time.Second * time.Duration(*status.HeartbeatFrequencySeconds) * ClusterNotReadyThreshold
	}
	return gracePeriod
}
