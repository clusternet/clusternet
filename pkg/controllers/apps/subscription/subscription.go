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

package subscription

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Subscription")

type SyncHandlerFunc func(subscription *appsapi.Subscription) error

// Controller is a controller that handles Subscription
type Controller struct {
	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	subsLister applisters.SubscriptionLister
	subsSynced cache.InformerSynced
	baseSynced cache.InformerSynced

	recorder record.EventRecorder

	syncHandlerFunc SyncHandlerFunc
}

// NewController returns a new Controller, which is only responsible for populating Bases.
// The scheduling parts are handled by clusternet-scheduler.
func NewController(clusternetClient clusternetclientset.Interface,
	subsInformer appinformers.SubscriptionInformer, baseInformer appinformers.BaseInformer,
	recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "subscription"),
		subsLister:       subsInformer.Lister(),
		subsSynced:       subsInformer.Informer().HasSynced,
		baseSynced:       baseInformer.Informer().HasSynced,
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}

	// Manage the addition/update of Subscription
	subsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addSubscription,
		UpdateFunc: c.updateSubscription,
		DeleteFunc: c.deleteSubscription,
	})

	baseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteBase,
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

	klog.Info("starting subscription controller...")
	defer klog.Info("shutting down subscription controller")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("subscription-controller", stopCh,
		c.subsSynced,
		c.baseSynced,
	) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Subscription resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addSubscription(obj interface{}) {
	sub := obj.(*appsapi.Subscription)
	klog.V(4).Infof("adding Subscription %q", klog.KObj(sub))
	c.enqueue(sub)
}

func (c *Controller) updateSubscription(old, cur interface{}) {
	oldSub := old.(*appsapi.Subscription)
	newSub := cur.(*appsapi.Subscription)

	if newSub.DeletionTimestamp != nil {
		c.enqueue(newSub)
		return
	}

	// Decide whether discovery has reported a status change.
	// clusternet-scheduler is responsible for spec changes.
	if reflect.DeepEqual(oldSub.Status, newSub.Status) {
		klog.V(4).Infof("no updates on the status of Subscription %s, skipping syncing", klog.KObj(oldSub))
		return
	}
	// no changes on binding namespaces and spec hash
	if reflect.DeepEqual(oldSub.Status.BindingClusters, newSub.Status.BindingClusters) &&
		reflect.DeepEqual(oldSub.Status.Replicas, newSub.Status.Replicas) &&
		oldSub.Status.SpecHash == newSub.Status.SpecHash {
		klog.V(4).Infof("no changes on binding namespaces and spec hash of Subscription %s, skipping syncing", klog.KObj(oldSub))
		return
	}

	klog.V(4).Infof("updating Subscription %q", klog.KObj(oldSub))
	c.enqueue(newSub)
}

func (c *Controller) deleteSubscription(obj interface{}) {
	sub, ok := obj.(*appsapi.Subscription)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		sub, ok = tombstone.Obj.(*appsapi.Subscription)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Subscription %#v", obj))
			return
		}
	}

	klog.V(4).Infof("deleting Subscription %q", klog.KObj(sub))
	c.enqueue(sub)
}

func (c *Controller) deleteBase(obj interface{}) {
	base, ok := obj.(*appsapi.Base)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		base, ok = tombstone.Obj.(*appsapi.Base)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Base %#v", obj))
			return
		}
	}

	sub := c.resolveControllerRef(base.Labels[known.ConfigNameLabel], base.Labels[known.ConfigNamespaceLabel], types.UID(base.Labels[known.ConfigUIDLabel]))
	if sub == nil {
		return
	}
	klog.V(4).Infof("deleting Base %q", klog.KObj(base))
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

	// add finalizer
	if !utils.ContainsString(sub.Finalizers, known.AppFinalizer) && sub.DeletionTimestamp == nil {
		sub.Finalizers = append(sub.Finalizers, known.AppFinalizer)
		if sub, err = c.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Update(context.TODO(),
			sub, metav1.UpdateOptions{}); err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Subscription %s: %v", known.AppFinalizer, klog.KObj(sub), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(sub, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Subscription %s", known.AppFinalizer, klog.KObj(sub))
		klog.V(4).Info(msg)
		c.recorder.Event(sub, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	// label Subscription self uid
	if val, ok := sub.Labels[string(sub.UID)]; !ok || val != controllerKind.Kind {
		sub, err = c.patchSubscriptionLabels(sub, map[string]*string{
			string(sub.UID): utilpointer.StringPtr(controllerKind.Kind),
		})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to patch Subscription %s labels: %v", klog.KObj(sub), err))
			return err
		}
	}

	sub.Kind = controllerKind.Kind
	sub.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(sub)
	if err != nil {
		c.recorder.Event(sub, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(sub, corev1.EventTypeNormal, "Synced", "Subscription synced successfully")
	}
	return err
}

func (c *Controller) UpdateSubscriptionStatus(sub *appsapi.Subscription, status *appsapi.SubscriptionStatus) error {
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

func (c *Controller) patchSubscriptionLabels(sub *appsapi.Subscription, labels map[string]*string) (*appsapi.Subscription, error) {
	if sub.DeletionTimestamp != nil || len(labels) == 0 {
		return sub, nil
	}

	klog.V(5).Infof("patching Subscription %s labels", klog.KObj(sub))
	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := json.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Patch(context.TODO(),
		sub.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}
