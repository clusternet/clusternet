/*
Copyright 2022 The Clusternet Authors.

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

package aggregatestatus

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
)

const (
	// DeploymentKind defined resource is a deployment
	DeploymentKind = "Deployment"
	// StatefulSetKind defined resource is a statefulset
	StatefulSetKind = "StatefulSet"
	// JobKind defined resource is a job
	JobKind = "Job"
)

// Controller is a controller that handle Description
type Controller struct {
	clusternetClient clusternetClientSet.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	descLister applisters.DescriptionLister
	descSynced cache.InformerSynced
	subsLister applisters.SubscriptionLister
	subsSynced cache.InformerSynced

	recorder record.EventRecorder
}

func NewController(clusternetClient clusternetClientSet.Interface,
	subsInformer appinformers.SubscriptionInformer, descInformer appinformers.DescriptionInformer,
	recorder record.EventRecorder) (*Controller, error) {

	c := &Controller{
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "aggregatestatus"),
		subsLister:       subsInformer.Lister(),
		subsSynced:       subsInformer.Informer().HasSynced,
		descLister:       descInformer.Lister(),
		descSynced:       descInformer.Informer().HasSynced,
		recorder:         recorder,
	}

	// Manage the update/delete of Description
	descInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.updateDescription,
		DeleteFunc: c.deleteDescription,
	})

	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("starting aggregateStatus controller...")
	defer klog.Info("shutting down aggregatestatus controller")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("aggregatestatus-controller", stopCh, c.subsSynced, c.descSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Subscription resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) updateDescription(old, cur interface{}) {
	oldDesc := old.(*appsapi.Description)
	newDesc := cur.(*appsapi.Description)

	sub := c.resolveControllerRef(oldDesc.Labels[known.ConfigSubscriptionNameLabel], oldDesc.Labels[known.ConfigSubscriptionNamespaceLabel],
		types.UID(oldDesc.Labels[known.ConfigSubscriptionUIDLabel]))
	if sub == nil {
		return
	}

	if newDesc.DeletionTimestamp != nil {
		c.enqueue(sub)
		return
	}

	// Decide whether discovery has reported a status change.
	if reflect.DeepEqual(oldDesc.Status, newDesc.Status) {
		klog.V(4).Infof("no updates on the spec of Description %s, skipping syncing", klog.KObj(oldDesc))
		return
	}

	klog.V(4).Infof("updating Description Status %q", klog.KObj(oldDesc))
	c.enqueue(sub)

}

func (c *Controller) deleteDescription(obj interface{}) {
	desc, ok := obj.(*appsapi.Description)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		desc, ok = tombstone.Obj.(*appsapi.Description)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Description %#v", obj))
			return
		}
	}

	sub := c.resolveControllerRef(desc.Labels[known.ConfigSubscriptionNameLabel], desc.Labels[known.ConfigSubscriptionNamespaceLabel],
		types.UID(desc.Labels[known.ConfigSubscriptionUIDLabel]))
	if sub == nil {
		return
	}

	klog.V(4).Infof("deleting Description %q", klog.KObj(desc))
	c.enqueue(sub)
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *Controller) resolveControllerRef(name, namespace string, uid types.UID) *appsapi.Subscription {
	sub, err := c.subsLister.Subscriptions(namespace).Get(name)
	if err != nil {
		return nil
	}
	if sub.UID != uid {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return sub
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

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
		// Subscription resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced Subscription %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Subscription resource
// with the current status of the resource.
func (c *Controller) syncHandler(key string) error {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	klog.V(4).Infof("start processing Subscription %q", key)
	// Get the Subscription resource with this name
	sub, err := c.subsLister.Subscriptions(ns).Get(name)
	// The Subscription resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Subscription %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	err = c.handleAggregateStatus(sub)
	if err != nil {
		c.recorder.Event(sub, corev1.EventTypeWarning, "FailedAggregateStatus", err.Error())
	} else {
		c.recorder.Event(sub, corev1.EventTypeNormal, "AggregateStatus", "Subscription synced successfully")
	}
	return err
}

func (c *Controller) updateSubscriptionStatus(sub *appsapi.Subscription, status *appsapi.SubscriptionStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update Subscription %q status", sub.Name)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		sub.Status = *status
		_, err := c.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).UpdateStatus(context.TODO(), sub, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return nil
		}

		if updated, err := c.subsLister.Subscriptions(sub.Namespace).Get(sub.Name); err == nil {
			// make a copy so we don't mutate the shared cache
			sub = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated Subscription %q from lister: %v", sub.Name, err))
		}
		return err
	})
}

// enqueue takes a Subscription resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Subscription.
func (c *Controller) enqueue(sub *appsapi.Subscription) {
	key, err := cache.MetaNamespaceKeyFunc(sub)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

// handleAggregateStatus
func (c *Controller) handleAggregateStatus(sub *appsapi.Subscription) error {
	klog.Infof("handle AggregateStatus %s", klog.KObj(sub))
	if sub.DeletionTimestamp != nil {
		return nil
	}

	aggregatedStatuses, err := c.descriptionStatusChanged(sub)
	if err == nil {
		if aggregatedStatuses != nil {
			subStatus := sub.Status.DeepCopy()
			subStatus.AggregatedStatuses = aggregatedStatuses

			err = c.updateSubscriptionStatus(sub, subStatus)
			if err != nil {
				klog.Errorf("Failed to aggregate baseStatus to Subscriptions(%s/%s). Error: %v.", sub.Namespace, sub.Name, err)
			}
		} else {
			klog.V(5).Infof("AggregateStatus %s does not change, skip it.", klog.KObj(sub))
		}
	}
	return err
}

func (c *Controller) descriptionStatusChanged(sub *appsapi.Subscription) ([]appsapi.AggregatedStatus, error) {
	allExistingDescs, listErr := c.descLister.List(labels.SelectorFromSet(labels.Set{
		known.ConfigSubscriptionNameLabel:      sub.Name,
		known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
		known.ConfigSubscriptionUIDLabel:       string(sub.UID),
	}))
	if listErr != nil {
		return nil, listErr
	}

	aggregatedStatusMap, clusterIndexMap, err := c.initaggregatedStatusMap(sub)
	if err != nil {
		return nil, err
	}
	for _, desc := range allExistingDescs {
		if desc.DeletionTimestamp != nil {
			continue
		}

		for _, manifeststatus := range desc.Status.ManifestStatuses {
			feedStatus := appsapi.FeedStatus{
				Available: true,
			}

			ReplicaStatus, err := c.getWorkloadReplicaStatus(manifeststatus)
			if err != nil {
				return nil, err
			}
			if ReplicaStatus != nil {
				feedStatus.ReplicaStatus = *ReplicaStatus
			}

			feedStatusOneCluster := appsapi.FeedStatusPerCluster{
				ClusterID:   types.UID(desc.Labels[known.ClusterIDLabel]),
				ClusterName: desc.Labels[known.ClusterNameLabel],
				FeedStatus:  feedStatus,
			}
			aggregatedStatus, hasfeed := aggregatedStatusMap[manifeststatus.Feed]
			index, hasindex := clusterIndexMap[string(feedStatusOneCluster.ClusterID)+"/"+feedStatusOneCluster.ClusterName]
			if !hasindex || !hasfeed {
				return nil, fmt.Errorf("error hasfeed: %t, hasindex: %t", hasfeed, hasindex)
			}
			aggregatedStatus.FeedStatusDetails[index] = feedStatusOneCluster
		}
	}

	NewaggregatedStatusMap := c.GeneratefeedStatusSummary(aggregatedStatusMap)

	values := make([]appsapi.AggregatedStatus, 0)
	for _, feed := range sub.Spec.Feeds {
		values = append(values, *NewaggregatedStatusMap[feed])
	}

	if reflect.DeepEqual(sub.Status.AggregatedStatuses, values) {
		klog.V(4).Infof("New aggregatedStatuses are equal with old subscription(%s/%s) AggregatedStatus, no update required.",
			sub.Namespace, sub.Name)
		return nil, nil
	}
	return values, nil
}

func (c *Controller) initaggregatedStatusMap(sub *appsapi.Subscription) (map[appsapi.Feed]*appsapi.AggregatedStatus, map[string]int, error) {
	initAggregatedStatusMap := make(map[appsapi.Feed]*appsapi.AggregatedStatus, 0)
	initFeedStatusDetails := make([]appsapi.FeedStatusPerCluster, 0)
	clusterIndexMap := make(map[string]int, 0)
	for _, binding := range sub.Status.BindingClusters {
		parts := strings.Split(binding, "/")
		if len(parts) < 2 {
			continue
		}
		namespace := parts[0]
		name := parts[1]
		managedCluster, err := c.clusternetClient.ClustersV1beta1().ManagedClusters(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			klog.Errorf("failed to list ManagedCluster in namespace %s: %v", namespace, err)
			return nil, nil, err
		}
		initFeedStatusDetails = append(initFeedStatusDetails, appsapi.FeedStatusPerCluster{
			ClusterID:   managedCluster.Spec.ClusterID,
			ClusterName: name,
		})
	}
	for _, feed := range sub.Spec.Feeds {
		feedStatusDetails := make([]appsapi.FeedStatusPerCluster, len(initFeedStatusDetails))
		copy(feedStatusDetails, initFeedStatusDetails)
		initAggregatedStatusMap[feed] = &appsapi.AggregatedStatus{
			Feed:              feed,
			FeedStatusDetails: feedStatusDetails,
		}
	}

	for index, oneCluster := range initFeedStatusDetails {
		clusterIndexMap[string(oneCluster.ClusterID)+"/"+oneCluster.ClusterName] = index
	}
	return initAggregatedStatusMap, clusterIndexMap, nil
}

func (c *Controller) GeneratefeedStatusSummary(aggregatedStatusMap map[appsapi.Feed]*appsapi.AggregatedStatus) map[appsapi.Feed]*appsapi.AggregatedStatus {
	newaggregatedStatusMap := make(map[appsapi.Feed]*appsapi.AggregatedStatus, 0)
	for feed, aggregatedStatus := range aggregatedStatusMap {
		feedStatus := appsapi.FeedStatus{
			Available: true,
		}
		for _, feedStatusOneCluster := range aggregatedStatus.FeedStatusDetails {
			feedStatus.Available = feedStatus.Available && feedStatusOneCluster.Available
			feedStatus.Replicas += feedStatusOneCluster.Replicas
			feedStatus.UpdatedReplicas += feedStatusOneCluster.UpdatedReplicas
			feedStatus.CurrentReplicas += feedStatusOneCluster.CurrentReplicas
			feedStatus.ReadyReplicas += feedStatusOneCluster.ReadyReplicas
			feedStatus.AvailableReplicas += feedStatusOneCluster.AvailableReplicas
			feedStatus.UnavailableReplicas += feedStatusOneCluster.UnavailableReplicas
			feedStatus.Active += feedStatusOneCluster.Active
			feedStatus.Succeeded += feedStatusOneCluster.Succeeded
			feedStatus.Failed += feedStatusOneCluster.Failed

			if feedStatusOneCluster.ObservedGeneration > feedStatus.ObservedGeneration {
				feedStatus.ObservedGeneration = feedStatusOneCluster.ObservedGeneration
			}
		}
		newaggregatedStatusMap[feed] = &appsapi.AggregatedStatus{
			Feed:              feed,
			FeedStatusSummary: feedStatus,
			FeedStatusDetails: aggregatedStatus.DeepCopy().FeedStatusDetails,
		}
	}

	return newaggregatedStatusMap
}

func (c *Controller) getWorkloadReplicaStatus(manifeststatus appsapi.ManifestStatus) (*appsapi.ReplicaStatus, error) {
	switch manifeststatus.Kind {
	case DeploymentKind:
		return c.getDeploymentReplicaStatus(manifeststatus)
	case StatefulSetKind:
		return c.getStatefulSetReplicaStatus(manifeststatus)
	case JobKind:
		return c.getJobReplicaStatus(manifeststatus)
	default:
		klog.V(5).Infof("unsupport kind(%s) resource(%s/%s) current, skip.",
			manifeststatus.Kind, manifeststatus.Namespace, manifeststatus.Name)
	}
	return nil, nil
}

func (c *Controller) getDeploymentReplicaStatus(manifeststatus appsapi.ManifestStatus) (*appsapi.ReplicaStatus, error) {
	if manifeststatus.APIVersion != "apps/v1" {
		return nil, fmt.Errorf("feed %s  apiVersion is not apps/v1", manifeststatus.Feed)
	}

	temp := &appsv1.DeploymentStatus{}
	if err := json.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
		klog.Errorf("Failed to unmarshal ObservedStatus status")
		return nil, err
	}
	klog.V(3).Infof("Get deployment(%s/%s) status, replicas: %d, ready: %d, updated: %d, available: %d, unavailable: %d",
		manifeststatus.Namespace, manifeststatus.Name, temp.Replicas, temp.ReadyReplicas, temp.UpdatedReplicas,
		temp.AvailableReplicas, temp.UnavailableReplicas)

	replicaStatus := &appsapi.ReplicaStatus{
		ObservedGeneration:  temp.ObservedGeneration,
		Replicas:            temp.Replicas,
		UpdatedReplicas:     temp.UpdatedReplicas,
		ReadyReplicas:       temp.ReadyReplicas,
		AvailableReplicas:   temp.AvailableReplicas,
		UnavailableReplicas: temp.UnavailableReplicas,
	}

	return replicaStatus, nil
}

func (c *Controller) getStatefulSetReplicaStatus(manifeststatus appsapi.ManifestStatus) (*appsapi.ReplicaStatus, error) {
	if manifeststatus.APIVersion != "apps/v1" {
		return nil, fmt.Errorf("feed %s  apiVersion is not apps/v1", manifeststatus.Feed)
	}

	temp := &appsv1.StatefulSetStatus{}
	if err := json.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
		klog.Errorf("Failed to unmarshal status")
		return nil, err
	}
	klog.V(3).Infof("Get StatefulSet(%s/%s) status, replicas: %d, ready: %d, updated: %d",
		manifeststatus.Namespace, manifeststatus.Name, temp.Replicas, temp.ReadyReplicas, temp.UpdatedReplicas)

	replicaStatus := &appsapi.ReplicaStatus{
		ObservedGeneration: temp.ObservedGeneration,
		Replicas:           temp.Replicas,
		UpdatedReplicas:    temp.UpdatedReplicas,
		CurrentReplicas:    temp.CurrentReplicas,
		ReadyReplicas:      temp.ReadyReplicas,
		AvailableReplicas:  temp.AvailableReplicas,
	}

	return replicaStatus, nil
}

func (c *Controller) getJobReplicaStatus(manifeststatus appsapi.ManifestStatus) (*appsapi.ReplicaStatus, error) {
	if manifeststatus.APIVersion != "batch/v1" {
		return nil, fmt.Errorf("feed %s  apiVersion is not batch/v1", manifeststatus.Feed)
	}

	temp := &batchv1.JobStatus{}
	if err := json.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
		klog.Errorf("Failed to unmarshal status")
		return nil, err
	}
	klog.V(3).Infof("Get Job(%s/%s) status, Active: %d, Succeeded: %d, Failed: %d",
		manifeststatus.Namespace, manifeststatus.Name, temp.Active, temp.Succeeded, temp.Failed)

	replicaStatus := &appsapi.ReplicaStatus{
		Active:    temp.Active,
		Succeeded: temp.Succeeded,
		Failed:    temp.Failed,
	}

	return replicaStatus, nil
}
