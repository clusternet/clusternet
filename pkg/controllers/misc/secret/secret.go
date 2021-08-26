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

package secret

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreInformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	coreListers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = corev1.SchemeGroupVersion.WithKind("Secret")

type SyncHandlerFunc func(secret *corev1.Secret) error

// Controller is a controller that handle Secret
// here we only foucs on Secret "child-cluster-deployer" !!!
type Controller struct {
	ctx context.Context

	kubeclient kubernetes.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	secretLister coreListers.SecretLister
	secretSynced cache.InformerSynced

	recorder    record.EventRecorder
	SyncHandler SyncHandlerFunc
}

func NewController(ctx context.Context, kubeclient kubernetes.Interface,
	secretInformer coreInformers.SecretInformer,
	recorder record.EventRecorder, syncHandler SyncHandlerFunc) (*Controller, error) {
	if syncHandler == nil {
		return nil, fmt.Errorf("syncHandler must be set")
	}

	c := &Controller{
		ctx:          ctx,
		kubeclient:   kubeclient,
		workqueue:    workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "secret"),
		secretLister: secretInformer.Lister(),
		secretSynced: secretInformer.Informer().HasSynced,
		recorder:     recorder,
		SyncHandler:  syncHandler,
	}

	// Manage the addition/update of Secret
	// here we only foucs on Secret "child-cluster-deployer" !!!
	secretInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addSecret,
		UpdateFunc: c.updateSecret,
		DeleteFunc: c.deleteSecret,
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

	klog.Info("starting Secret controller...")
	defer klog.Info("shutting down Secret controller")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.secretSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Secret resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addSecret(obj interface{}) {
	secret := obj.(*corev1.Secret)

	if secret.Name != known.ChildClusterSecretName {
		return
	}

	// add finalizer
	if secret.DeletionTimestamp == nil {
		if !utils.ContainsString(secret.Finalizers, known.AppFinalizer) {
			secret.Finalizers = append(secret.Finalizers, known.AppFinalizer)
		}
		_, err := c.kubeclient.CoreV1().Secrets(secret.Namespace).Update(context.TODO(),
			secret, metav1.UpdateOptions{})
		if err == nil {
			msg := fmt.Sprintf("successfully inject finalizer %s to Secret %s", known.AppFinalizer, klog.KObj(secret))
			klog.V(4).Info(msg)
			return
		} else {
			klog.WarningDepth(4,
				fmt.Sprintf("failed to inject finalizer %s to Secret %s: %v", known.AppFinalizer, klog.KObj(secret), err))
			c.addSecret(obj)
			return
		}
	}

	c.enqueue(secret)
}

func (c *Controller) updateSecret(old, cur interface{}) {
	secret := cur.(*corev1.Secret)
	if secret.Name != known.ChildClusterSecretName {
		return
	}

	if secret.DeletionTimestamp != nil {
		c.enqueue(secret)
		return
	}
}

func (c *Controller) deleteSecret(obj interface{}) {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		secret, ok = tombstone.Obj.(*corev1.Secret)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Secret %#v", obj))
			return
		}
	}

	if secret.Name != known.ChildClusterSecretName {
		return
	}

	klog.V(4).Infof("deleting Secret %q", klog.KObj(secret))
	c.enqueue(secret)
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
		// Secret resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced Secret %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Secret resource
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

	klog.V(4).Infof("start processing Secret %q", key)
	// Get the Secret resource with this name
	secret, err := c.secretLister.Secrets(ns).Get(name)
	// The Secret resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Secret %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	secret.Kind = controllerKind.Kind
	secret.APIVersion = controllerKind.Version

	err = c.SyncHandler(secret)
	if err != nil {
		c.recorder.Event(secret, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(secret, corev1.EventTypeNormal, "Synced", "Secret synced successfully")
	}
	return err
}

// enqueue takes a Secret resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Secret.
func (c *Controller) enqueue(secret *corev1.Secret) {
	key, err := cache.MetaNamespaceKeyFunc(secret)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}
