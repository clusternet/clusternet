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

package resource

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

type SyncHandlerFunc func(resAttrs *ResourceAttrs) error

const (
	// ResourceEvent Add Type
	ObjectAdd = "Add"
	// ResourceEvent Update resource status
	ObjectUpdateStatus = "UpdateStatus"
	// ResourceEvent Update resource spec
	ObjectUpdateOthers = "ObjectUpdateOthers"
	// ResourceEvent Delete Type
	ObjectDelete = "Delete"
)

// ResourceAttrs
type ResourceAttrs struct {
	Namespace    string
	Name         string
	ObjectAction string
}

// Controller is a controller that handles `name` specified resource
type Controller struct {
	workqueue        workqueue.RateLimitingInterface
	client           utils.ResourceClient
	resourceSynced   cache.InformerSynced
	syncHandlerFunc  SyncHandlerFunc
	name             string
	clusternetClient *versioned.Clientset
}

func getResourceAttrs(namespaceName string, action string) (*ResourceAttrs, error) {
	parts := strings.Split(namespaceName, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("split namespaceName error")
	}
	resAttrs := &ResourceAttrs{
		Namespace:    parts[0],
		Name:         parts[1],
		ObjectAction: action,
	}
	return resAttrs, nil
}

func NewController(apiResource *metav1.APIResource, clusternetClient *versioned.Clientset,
	client dynamic.Interface, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}
	gvk := schema.GroupVersionKind{Group: apiResource.Group,
		Version: apiResource.Version, Kind: apiResource.Kind}

	resourceClient := utils.NewResourceClient(client, apiResource)

	c := &Controller{
		syncHandlerFunc:  syncHandlerFunc,
		client:           resourceClient,
		name:             gvk.String(),
		clusternetClient: clusternetClient,
		workqueue: workqueue.NewRateLimitingQueueWithConfig(
			workqueue.DefaultControllerRateLimiter(),
			workqueue.RateLimitingQueueConfig{Name: gvk.String()},
		),
	}

	ri, err := utils.NewResourceInformer(resourceClient, apiResource, cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			resource := obj.(*unstructured.Unstructured)
			val, ok := resource.GetAnnotations()[known.ObjectOwnedByDescriptionAnnotation]
			resAttrs, err := getResourceAttrs(val, ObjectAdd)
			if ok && err == nil {
				klog.V(4).Infof("adding %s %q", c.name, klog.KObj(resource))
				c.enqueue(resAttrs)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldresource := oldObj.(*unstructured.Unstructured)
			newresource := newObj.(*unstructured.Unstructured)
			oldstatus, _, _ := unstructured.NestedMap(oldresource.Object, "status")
			newstatus, _, _ := unstructured.NestedMap(newresource.Object, "status")
			oldspec, _, _ := unstructured.NestedMap(oldresource.Object, "spec")
			newspec, _, _ := unstructured.NestedMap(newresource.Object, "spec")
			val, ok := oldresource.GetAnnotations()[known.ObjectOwnedByDescriptionAnnotation]

			var resAttrs *ResourceAttrs
			var err error
			if !reflect.DeepEqual(oldspec, newspec) {
				resAttrs, err = getResourceAttrs(val, ObjectUpdateOthers)
			} else if !reflect.DeepEqual(oldstatus, newstatus) {
				resAttrs, err = getResourceAttrs(val, ObjectUpdateStatus)
			} else {
				resAttrs, err = getResourceAttrs(val, ObjectUpdateOthers)
			}

			if ok && err == nil {
				klog.V(4).Infof("updating %s %q", c.name, klog.KObj(oldresource))
				c.enqueue(resAttrs)
			}
		},
		DeleteFunc: func(obj interface{}) {
			var resource *unstructured.Unstructured
			switch t := obj.(type) {
			case *unstructured.Unstructured:
				resource = obj.(*unstructured.Unstructured)
			case cache.DeletedFinalStateUnknown:
				var ok bool
				resource, ok = t.Obj.(*unstructured.Unstructured)
				if !ok {
					utilruntime.HandleError(fmt.Errorf("unable to convert object %T to *unstructured.Unstructured", obj))
					return
				}
			default:
				utilruntime.HandleError(fmt.Errorf("unable to handle object %T", obj))
				return
			}

			val, ok := resource.GetAnnotations()[known.ObjectOwnedByDescriptionAnnotation]
			resAttrs, err := getResourceAttrs(val, ObjectDelete)
			if ok && err == nil {
				klog.V(4).Infof("deleting %s %q", c.name, klog.KObj(resource))
				c.enqueue(resAttrs)
			}
		},
	})
	if err != nil {
		return nil, err
	}
	ri.Start()
	c.resourceSynced = ri.HasSynced

	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Infof("starting %s controller...", c.name)
	defer klog.Infof("shutting down %s controller", c.name)

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync(c.name+"-controller", stopCh, c.resourceSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
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
		var key *ResourceAttrs
		var ok bool
		if key, ok = obj.(*ResourceAttrs); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) syncHandler(resAttrs *ResourceAttrs) error {
	if resAttrs == nil {
		return fmt.Errorf("error resAttrs is nil")
	}

	klog.V(4).Infof("start processing Description %s/%s", resAttrs.Namespace, resAttrs.Name)
	return c.syncHandlerFunc(resAttrs)
}

// enqueue puts key onto the work queue.
func (c *Controller) enqueue(resAttrs *ResourceAttrs) {
	c.workqueue.Add(resAttrs)
}
