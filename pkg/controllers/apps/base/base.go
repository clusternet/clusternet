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

package base

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
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Base")

type SyncHandlerFunc func(orig *appsapi.Base) error

// Controller is a controller that handle Base
type Controller struct {
	ctx context.Context

	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	baseLister applisters.BaseLister
	baseSynced cache.InformerSynced

	recorder record.EventRecorder

	syncHandlerFunc SyncHandlerFunc
}

func NewController(ctx context.Context, clusternetClient clusternetclientset.Interface,
	baseInformer appinformers.BaseInformer, descInformer appinformers.DescriptionInformer,
	recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		ctx:              ctx,
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "base"),
		baseLister:       baseInformer.Lister(),
		baseSynced:       baseInformer.Informer().HasSynced,
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}

	// Manage the addition/update of Base
	baseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addBase,
		UpdateFunc: c.updateBase,
		DeleteFunc: c.deleteBase,
	})

	descInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
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

	klog.Info("starting base controller...")
	defer klog.Info("shutting down base controller")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.baseSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Base resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addBase(obj interface{}) {
	base := obj.(*appsapi.Base)
	klog.V(4).Infof("adding Base %q", klog.KObj(base))

	// add finalizer
	if !utils.ContainsString(base.Finalizers, known.AppFinalizer) && base.DeletionTimestamp == nil {
		base.Finalizers = append(base.Finalizers, known.AppFinalizer)
		_, err := c.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Update(context.TODO(),
			base, metav1.UpdateOptions{})
		if err == nil {
			msg := fmt.Sprintf("successfully inject finalizer %s to Base %s", known.AppFinalizer, klog.KObj(base))
			klog.V(4).Info(msg)
			c.recorder.Event(base, corev1.EventTypeNormal, "FinalizerInjected", msg)
		} else {
			msg := fmt.Sprintf("failed to inject finalizer %s to Base %s: %v", known.AppFinalizer, klog.KObj(base), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(base, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			c.addBase(obj)
			return
		}
	}

	// label Base self uid
	if val, ok := base.Labels[string(base.UID)]; !ok || val != controllerKind.Kind {
		err := c.patchBaseLabels(base, map[string]*string{
			string(base.UID): utilpointer.StringPtr(controllerKind.Kind),
		})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to patch Base labels: %v", err))
			c.addBase(obj)
			return
		}
	}

	c.enqueue(base)
}

func (c *Controller) updateBase(old, cur interface{}) {
	oldBase := old.(*appsapi.Base)
	newBase := cur.(*appsapi.Base)

	if newBase.DeletionTimestamp != nil {
		c.enqueue(newBase)
		return
	}

	// label Base self uid
	if val, ok := newBase.Labels[string(newBase.UID)]; !ok || val != controllerKind.Kind {
		err := c.patchBaseLabels(newBase, map[string]*string{
			string(newBase.UID): utilpointer.StringPtr(controllerKind.Kind),
		})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to patch Base labels: %v", err))
			c.updateBase(old, cur)
			return
		}
	}

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldBase.Spec, newBase.Spec) {
		klog.V(4).Infof("no updates on the spec of Base %q, skipping syncing", oldBase.Name)
		return
	}

	klog.V(4).Infof("updating Base %q", klog.KObj(oldBase))
	c.enqueue(newBase)
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
	klog.V(4).Infof("deleting Base %q", klog.KObj(base))
	c.enqueue(base)
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

	controllerRef := &metav1.OwnerReference{
		Kind: desc.Labels[known.ConfigKindLabel],
		Name: desc.Labels[known.ConfigNameLabel],
		UID:  types.UID(desc.Labels[known.ConfigUIDLabel]),
	}
	base := c.resolveControllerRef(desc.Labels[known.ConfigNamespaceLabel], controllerRef)
	if base == nil {
		return
	}
	klog.V(4).Infof("deleting Description %q", klog.KObj(desc))
	c.enqueue(base)
}

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *Controller) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *appsapi.Base {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != controllerKind.Kind {
		return nil
	}

	base, err := c.baseLister.Bases(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if base.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return base
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
		// Base resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced Base %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Base resource
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

	klog.V(4).Infof("start processing Base %q", key)
	// Get the Base resource with this name
	base, err := c.baseLister.Bases(ns).Get(name)
	// The Base resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Base %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	base.Kind = controllerKind.Kind
	base.APIVersion = controllerKind.Version

	return c.syncHandlerFunc(base)
}

// enqueue takes a Base resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Base.
func (c *Controller) enqueue(base *appsapi.Base) {
	key, err := cache.MetaNamespaceKeyFunc(base)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) patchBaseLabels(base *appsapi.Base, labels map[string]*string) error {
	klog.V(5).Infof("patching Base labels")
	option := utils.LabelOption{Meta: utils.Meta{Labels: labels}}
	patchData, err := json.Marshal(option)
	if err != nil {
		return err
	}

	_, err = c.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Patch(context.TODO(),
		base.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
	return err
}
