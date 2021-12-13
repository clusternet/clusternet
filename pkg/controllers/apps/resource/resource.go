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
	"context"
	"fmt"

	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

type SyncHandlerFunc func(obj interface{}) error

// Controller is a controller that handle `name` specified resource
type Controller struct {
	workqueue        workqueue.RateLimitingInterface
	client           utils.ResourceClient
	resourceSynced   cache.InformerSynced
	syncHandlerFunc  SyncHandlerFunc
	name             string
	clusternetClient *versioned.Clientset
}

func NewController(apiResource *metav1.APIResource, namespace string, clusternetClient *versioned.Clientset,
	client dynamic.Interface, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}
	gvk := schema.GroupVersionKind{Group: apiResource.Group,
		Version: apiResource.Version, Kind: apiResource.Kind}

	resourceClient, _ := utils.NewResourceClient(client, apiResource)

	c := &Controller{
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), gvk.String()),
		syncHandlerFunc:  syncHandlerFunc,
		client:           resourceClient,
		name:             gvk.String(),
		clusternetClient: clusternetClient,
	}

	ri := utils.NewResourceInformer(resourceClient, namespace, apiResource, cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addResource,
		UpdateFunc: c.updateResource,
		DeleteFunc: c.deleteResource,
	})
	ri.Start()
	c.resourceSynced = ri.HasSynced

	return c, nil
}

func (c *Controller) addResource(obj interface{}) {
	resource := obj.(*unstructured.Unstructured)
	klog.V(4).Infof("adding %s %q", c.name, klog.KObj(resource))
	c.enqueue(resource)
}

func (c *Controller) updateResource(old, cur interface{}) {
	curObj := cur.(*unstructured.Unstructured)
	currentName := curObj.GroupVersionKind().String() + curObj.GetNamespace() + curObj.GetName()

	if curObj.GetDeletionTimestamp() != nil {
		c.enqueue(curObj)
		return
	}

	desc, err := utils.ResolveDescriptionFromResource(c.clusternetClient, old)
	if err != nil {
		return
	}
	// in case the Description label been removed in child cluster.
	if _, ok := curObj.GetLabels()[known.ObjectControlledByLabel]; !ok {
		labels := curObj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[known.ObjectControlledByLabel] = desc.GetNamespace() + "." + desc.GetName()
		curObj.SetLabels(labels)
		if _, error := c.client.Resources(curObj.GetNamespace()).Update(context.TODO(), curObj, metav1.UpdateOptions{}); error != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to update rawResource: %v", err))
			return
		}
	}

	// in case the create label been removed in child cluster.
	if _, ok := curObj.GetLabels()[known.ObjectCreatedByLabel]; !ok {
		labels := curObj.GetLabels()
		if labels == nil {
			labels = map[string]string{}
		}
		labels[known.ObjectCreatedByLabel] = known.ClusternetHubName
		curObj.SetLabels(labels)
		if _, error := c.client.Resources(curObj.GetNamespace()).Update(context.TODO(), curObj, metav1.UpdateOptions{}); error != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to update rawResource: %v", err))
			return
		}
	}

	objectsToBeDeployed := desc.Spec.Raw
	for _, object := range objectsToBeDeployed {
		rawResource := &unstructured.Unstructured{}
		err := rawResource.UnmarshalJSON(object)
		if err != nil {
			msg := fmt.Sprintf("failed to unmarshal rawResource: %v", err)
			klog.ErrorDepth(5, msg)
		} else {
			rawName := rawResource.GroupVersionKind().String() + rawResource.GetNamespace() + rawResource.GetName()

			if rawName == currentName {
				if utils.ResourceNeedResync(rawResource, curObj) {
					c.enqueue(curObj)
					klog.V(4).Infof("updating rawResource %q", klog.KObj(curObj))
					return
				}
				klog.V(4).Infof("no updates on the object %s, skipping syncing", klog.KObj(curObj))
				break
			}
		}
	}
}

func (c *Controller) deleteResource(obj interface{}) {
	resource := obj.(*unstructured.Unstructured)
	if deleted, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		// This object might be stale but ok for our current usage.
		obj = deleted.Obj
		if obj == nil {
			return
		}
	}
	klog.V(4).Infof("deleting object %q", klog.KObj(resource))
	c.enqueue(resource)
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
		// resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced %q %q", c.name, key)
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
func (c *Controller) syncHandler(key string) error {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource %s key: %s", c.name, key))
		return nil
	}

	klog.V(4).Infof("start processing Resource %s %q", c.name, key)
	// Get the resource with this namespace/name
	obj, err := c.client.Resources(ns).Get(context.TODO(), name, metav1.GetOptions{})
	// The resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("%s %q has been deleted", c.name, key)
		return nil
	}
	if err != nil {
		return err
	}

	err = c.syncHandlerFunc(obj)

	return err
}

// enqueue takes a resource object and converts it into a namespace/name
// string which is then put onto the work queue. This method can handle any
// kind of resources.
func (c *Controller) enqueue(object runtime.Object) {
	key, err := cache.MetaNamespaceKeyFunc(object)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}
