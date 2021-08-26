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

package helmrelease

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("HelmRelease")

type SyncHandlerFunc func(helmrelease *appsapi.HelmRelease) error

// Controller is a controller that handle HelmRelease
type Controller struct {
	ctx context.Context

	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	descLister applisters.DescriptionLister
	descSynced cache.InformerSynced
	hrLister   applisters.HelmReleaseLister
	hrSynced   cache.InformerSynced

	recorder        record.EventRecorder
	syncHandlerFunc SyncHandlerFunc
}

func NewController(ctx context.Context, clusternetClient clusternetclientset.Interface,
	descInformer appinformers.DescriptionInformer, hrInformer appinformers.HelmReleaseInformer,
	recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		ctx:              ctx,
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "helmRelease"),
		descLister:       descInformer.Lister(),
		descSynced:       descInformer.Informer().HasSynced,
		hrLister:         hrInformer.Lister(),
		hrSynced:         hrInformer.Informer().HasSynced,
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}

	// Manage the addition/update of HelmRelease
	hrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addHelmRelease,
		UpdateFunc: c.updateHelmRelease,
		DeleteFunc: c.deleteHelmRelease,
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

	klog.Info("starting helmRelease controller...")
	defer klog.Info("shutting down helmRelease controller")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.hrSynced, c.descSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process HelmRelease resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addHelmRelease(obj interface{}) {
	hr := obj.(*appsapi.HelmRelease)
	klog.V(4).Infof("adding HelmRelease %q", klog.KObj(hr))
	c.enqueue(hr)
}

func (c *Controller) updateHelmRelease(old, cur interface{}) {
	oldHr := old.(*appsapi.HelmRelease)
	newHr := cur.(*appsapi.HelmRelease)

	if newHr.DeletionTimestamp != nil {
		c.enqueue(newHr)
		return
	}

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldHr.Spec, newHr.Spec) {
		klog.V(4).Infof("no updates on the spec of HelmRelease %q, skipping syncing", oldHr.Name)
		return
	}

	klog.V(4).Infof("updating HelmRelease %q", klog.KObj(oldHr))
	c.enqueue(newHr)
}

func (c *Controller) deleteHelmRelease(obj interface{}) {
	hr, ok := obj.(*appsapi.HelmRelease)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		hr, ok = tombstone.Obj.(*appsapi.HelmRelease)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a HelmRelease %#v", obj))
			return
		}
	}
	klog.V(4).Infof("deleting HelmRelease %q", klog.KObj(hr))
	c.enqueue(hr)
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
		// HelmRelease resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced HelmRelease %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the HelmRelease resource
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

	klog.V(4).Infof("start processing HelmRelease %q", key)
	// Get the HelmRelease resource with this name
	hr, err := c.hrLister.HelmReleases(ns).Get(name)
	// The HelmRelease resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("HelmRelease %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	hr.Kind = controllerKind.Kind
	hr.APIVersion = controllerKind.Version

	err = c.syncHandlerFunc(hr)
	if err != nil {
		c.recorder.Event(hr, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(hr, corev1.EventTypeNormal, "Synced", "HelmRelease synced successfully")
	}
	return err
}

func (c *Controller) UpdateHelmReleaseStatus(hr *appsapi.HelmRelease, status *appsapi.HelmReleaseStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update HelmRelease %q status", hr.Name)

	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		hr.Status = *status
		_, err := c.clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).UpdateStatus(c.ctx, hr, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}

		if updated, err := c.hrLister.HelmReleases(hr.Namespace).Get(hr.Name); err == nil {
			// make a copy so we don't mutate the shared cache
			hr = updated.DeepCopy()
		} else {
			utilruntime.HandleError(fmt.Errorf("error getting updated HelmRelease %q from lister: %v", hr.Name, err))
		}
		return err
	})

	if err != nil {
		return err
	}

	klog.V(5).Infof("try to update HelmRelease %q owner Description status", hr.Name)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		controllerRef := metav1.GetControllerOf(hr)
		if controllerRef == nil {
			// No controller should care about orphans being deleted.
			return nil
		}
		desc := c.resolveControllerRef(hr.Namespace, controllerRef)
		if desc == nil {
			return nil
		}
		if status.Phase == release.StatusDeployed {
			desc.Status.Phase = appsapi.DescriptionPhaseSuccess
			desc.Status.Reason = ""
		} else {
			desc.Status.Phase = appsapi.DescriptionPhaseFailure
			desc.Status.Reason = status.Notes
		}
		_, err := c.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).UpdateStatus(c.ctx, desc, metav1.UpdateOptions{})
		if err == nil {
			return nil
		}

		utilruntime.HandleError(fmt.Errorf("error updating status for Description %q: %v", klog.KObj(desc), err))
		return err
	})
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *Controller) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *appsapi.Description {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != "Description" {
		return nil
	}
	desc, err := c.descLister.Descriptions(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if desc.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return desc
}

// enqueue takes a HelmReleases resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than HelmRelease.
func (c *Controller) enqueue(hr *appsapi.HelmRelease) {
	key, err := cache.MetaNamespaceKeyFunc(hr)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}
