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
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dixudx/yacht"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// DeploymentKind defined resource is a deployment
	DeploymentKind = "Deployment"
	// StatefulSetKind defined resource is a statefulset
	StatefulSetKind = "StatefulSet"
	// JobKind defined resource is a job
	JobKind = "Job"
)

// Controller is a controller that handles Description
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetClientSet.Interface
	subsLister       applisters.SubscriptionLister
	descLister       applisters.DescriptionLister
	recorder         record.EventRecorder
}

func NewController(
	clusternetClient clusternetClientSet.Interface,
	subsInformer appinformers.SubscriptionInformer,
	descInformer appinformers.DescriptionInformer,
	recorder record.EventRecorder,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		subsLister:       subsInformer.Lister(),
		descLister:       descInformer.Lister(),
		recorder:         recorder,
	}
	// create a yacht controller for aggregateStatus
	yachtController := yacht.NewController("aggregateStatus").
		WithCacheSynced(
			subsInformer.Informer().HasSynced,
			descInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle)

	yachtController.WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
		// we explicitly call yachtController.Enqueue to only enqueue Subscription
		var oldDesc, newDesc *appsapi.Description
		var oldSubs, newSubs *appsapi.Subscription
		if oldObj != nil {
			oldDesc = oldObj.(*appsapi.Description)
			oldSubs = c.resolveControllerRef(
				oldDesc.Labels[known.ConfigSubscriptionNameLabel],
				oldDesc.Labels[known.ConfigSubscriptionNamespaceLabel],
				types.UID(oldDesc.Labels[known.ConfigSubscriptionUIDLabel]),
			)
		}
		if newObj != nil {
			newDesc = newObj.(*appsapi.Description)
			newSubs = c.resolveControllerRef(
				newDesc.Labels[known.ConfigSubscriptionNameLabel],
				newDesc.Labels[known.ConfigSubscriptionNamespaceLabel],
				types.UID(newDesc.Labels[known.ConfigSubscriptionUIDLabel]),
			)
		}

		// Decide whether discovery has reported a status change.
		if oldDesc != nil && newDesc != nil && reflect.DeepEqual(oldDesc.Status, newDesc.Status) {
			klog.V(4).Infof("no updates on the spec of Description %s, skipping syncing", klog.KObj(oldDesc))
			return false, nil
		}

		if oldSubs == nil && newSubs == nil {
			return false, nil
		}
		if oldSubs != nil {
			yachtController.Enqueue(oldSubs)
			return false, nil
		}
		if newSubs != nil {
			yachtController.Enqueue(newSubs)
			return false, nil
		}

		// always return false, since we are not enqueueing Description
		return false, nil
	})

	// Manage the addition/update of Description
	// We get informed of Description changes, but will enqueue matching Subscription instead
	_, err := descInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	c.yachtController = yachtController
	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, ctx context.Context) {
	c.yachtController.WithWorkers(workers).Run(ctx)
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

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Subscription resource
// with the current status of the resource.
func (c *Controller) handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	key := obj.(string)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing Subscription %q", key)
	// Get the Subscription resource with this name
	cachedSub, err := c.subsLister.Subscriptions(ns).Get(name)
	// The Subscription resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Subscription %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	subCopy := cachedSub.DeepCopy()
	err = c.handleAggregateStatus(subCopy)
	if err != nil {
		c.recorder.Event(subCopy, corev1.EventTypeWarning, "FailedAggregateStatus", err.Error())
	} else {
		c.recorder.Event(subCopy, corev1.EventTypeNormal, "AggregateStatus", "Subscription synced successfully")
		klog.Infof("successfully synced Subscription %q", key)
	}
	return nil, err
}

func (c *Controller) updateSubscriptionStatus(subCopy *appsapi.Subscription) error {
	klog.V(5).Infof("try to update Subscription %q status", subCopy.Name)
	status := subCopy.Status

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		_, err := c.clusternetClient.AppsV1alpha1().Subscriptions(subCopy.Namespace).UpdateStatus(context.TODO(), subCopy, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return nil
		}

		updated, err2 := c.subsLister.Subscriptions(subCopy.Namespace).Get(subCopy.Name)
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			subCopy = updated.DeepCopy()
			subCopy.Status = status
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated Subscription %q from lister: %v", subCopy.Name, err2))
		}
		return err2
	})
}

// handleAggregateStatus
func (c *Controller) handleAggregateStatus(subCopy *appsapi.Subscription) error {
	klog.Infof("handle AggregateStatus %s", klog.KObj(subCopy))
	if subCopy.DeletionTimestamp != nil {
		return nil
	}

	aggregatedStatuses, err := c.descriptionStatusChanged(subCopy)
	if err == nil {
		if aggregatedStatuses != nil {
			subCopy.Status.AggregatedStatuses = aggregatedStatuses
			err = c.updateSubscriptionStatus(subCopy)
			if err != nil {
				klog.Errorf("Failed to aggregate baseStatus to Subscriptions(%s/%s). Error: %v.", subCopy.Namespace, subCopy.Name, err)
			}
		} else {
			klog.V(5).Infof("AggregateStatus %s does not change, skip it.", klog.KObj(subCopy))
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

			var replicaStatus *appsapi.ReplicaStatus
			replicaStatus, err = c.getWorkloadReplicaStatus(manifeststatus)
			if err != nil {
				return nil, err
			}
			if replicaStatus != nil {
				feedStatus.ReplicaStatus = *replicaStatus
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
	if err := utils.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
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
	if err := utils.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
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
	if err := utils.Unmarshal(manifeststatus.ObservedStatus.Raw, temp); err != nil {
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
