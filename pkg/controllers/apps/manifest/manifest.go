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

package manifest

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Manifest")

type SyncHandlerFunc func(manifest *appsapi.Manifest) error

// Controller is a controller that handle Manifest
type Controller struct {
	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	manifestLister applisters.ManifestLister
	manifestSynced cache.InformerSynced
	baseLister     applisters.BaseLister
	baseSynced     cache.InformerSynced

	feedInUseProtection bool

	recorder        record.EventRecorder
	syncHandlerFunc SyncHandlerFunc
}

func NewController(clusternetClient clusternetclientset.Interface,
	manifestInformer appinformers.ManifestInformer, baseInformer appinformers.BaseInformer,
	feedInUseProtection bool, recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		clusternetClient:    clusternetClient,
		workqueue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "manifest"),
		manifestLister:      manifestInformer.Lister(),
		manifestSynced:      manifestInformer.Informer().HasSynced,
		baseLister:          baseInformer.Lister(),
		baseSynced:          baseInformer.Informer().HasSynced,
		feedInUseProtection: feedInUseProtection,
		recorder:            recorder,
		syncHandlerFunc:     syncHandlerFunc,
	}

	// Manage the addition/update of Manifest
	manifestInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addManifest,
		UpdateFunc: c.updateManifest,
		DeleteFunc: c.deleteManifest,
	})

	baseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: c.updateBase,
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

	klog.Info("starting manifest controller...")
	defer klog.Info("shutting down manifest controller")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("manifest-controller", stopCh, c.manifestSynced, c.baseSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Manifest resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addManifest(obj interface{}) {
	manifest := obj.(*appsapi.Manifest)
	klog.V(4).Infof("adding Manifest %q", klog.KObj(manifest))
	c.enqueue(manifest)
}

func (c *Controller) updateManifest(old, cur interface{}) {
	oldManifest := old.(*appsapi.Manifest)
	newManifest := cur.(*appsapi.Manifest)

	if newManifest.DeletionTimestamp != nil {
		c.enqueue(newManifest)
		return
	}

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldManifest.Template, newManifest.Template) {
		klog.V(4).Infof("no updates on Manifest template %s, skipping syncing", klog.KObj(oldManifest))
		return
	}

	klog.V(4).Infof("updating Manifest %q", klog.KObj(oldManifest))
	c.enqueue(newManifest)
}

func (c *Controller) deleteManifest(obj interface{}) {
	manifest, ok := obj.(*appsapi.Manifest)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		manifest, ok = tombstone.Obj.(*appsapi.Manifest)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Manifest %#v", obj))
			return
		}
	}
	klog.V(4).Infof("deleting Manifest %q", klog.KObj(manifest))
	c.enqueue(manifest)
}

func (c *Controller) updateBase(old, cur interface{}) {
	oldBase := old.(*appsapi.Base)
	newBase := cur.(*appsapi.Base)

	if newBase.DeletionTimestamp != nil {
		// labels pruning had already been done when a Base got deleted.
		return
	}

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldBase.Spec.Feeds, newBase.Spec.Feeds) {
		return
	}

	klog.V(6).Info("waiting for Manifest caches to sync before pruning labels")
	if !cache.WaitForCacheSync(context.TODO().Done(), c.manifestSynced) {
		return
	}

	for _, feed := range utils.FindObsoletedFeeds(oldBase.Spec.Feeds, newBase.Spec.Feeds) {
		if feed.Kind == "HelmChart" {
			continue
		}

		manifests, err := utils.ListManifestsBySelector(c.manifestLister, feed)
		if err != nil {
			klog.ErrorDepth(6, err)
			continue
		}

		for _, manifest := range manifests {
			klog.V(6).Infof("pruning labels of Manifest %q", klog.KObj(manifest))
			c.workqueue.Add(klog.KObj(manifest).String())
		}
	}
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
		// Manifest resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced Manifest %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Manifest resource
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

	klog.V(4).Infof("start processing Manifest %q", key)
	// Get the Manifest resource with this name
	manifest, err := c.manifestLister.Manifests(ns).Get(name)
	// The Manifest resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Manifest %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if manifest.DeletionTimestamp == nil {
		updatedManifest := manifest.DeepCopy()

		// add finalizers
		if !utils.ContainsString(updatedManifest.Finalizers, known.AppFinalizer) {
			updatedManifest.Finalizers = append(updatedManifest.Finalizers, known.AppFinalizer)
		}
		if !utils.ContainsString(updatedManifest.Finalizers, known.FeedProtectionFinalizer) && c.feedInUseProtection {
			updatedManifest.Finalizers = append(updatedManifest.Finalizers, known.FeedProtectionFinalizer)
		}

		// prune redundant labels
		pruneLabels(updatedManifest, c.baseLister)

		// only update on changed
		if !reflect.DeepEqual(manifest, updatedManifest) {
			if manifest, err = c.clusternetClient.AppsV1alpha1().Manifests(updatedManifest.Namespace).Update(context.TODO(),
				updatedManifest, metav1.UpdateOptions{}); err != nil {
				msg := fmt.Sprintf("failed to inject finalizers to Manifest %s: %v", klog.KObj(manifest), err)
				klog.WarningDepth(4, msg)
				c.recorder.Event(manifest, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
				return err
			}
			msg := fmt.Sprintf("successfully inject finalizers to Manifest %s", klog.KObj(manifest))
			klog.V(4).Info(msg)
			c.recorder.Event(manifest, corev1.EventTypeNormal, "FinalizerInjected", msg)
		}
	}

	manifest.Kind = controllerKind.Kind
	manifest.APIVersion = controllerKind.Version
	err = c.syncHandlerFunc(manifest)
	if err != nil {
		c.recorder.Event(manifest, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(manifest, corev1.EventTypeNormal, "Synced", "Manifest synced successfully")
	}
	return err
}

// enqueue takes a Manifest resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Manifest.
func (c *Controller) enqueue(manifest *appsapi.Manifest) {
	key, err := cache.MetaNamespaceKeyFunc(manifest)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func pruneLabels(manifest *appsapi.Manifest, baseLister applisters.BaseLister) {
	// find all Bases
	baseUIDs := []string{}
	for key, val := range manifest.Labels {
		// normally the length of a uuid is 36
		if len(key) != 36 || strings.Contains(key, "/") || val != "Base" {
			continue
		}
		baseUIDs = append(baseUIDs, key)
	}

	for _, base := range utils.FindBasesFromUIDs(baseLister, baseUIDs) {
		if utils.HasFeed(appsapi.Feed{
			Kind:      manifest.Labels[known.ConfigKindLabel],
			Namespace: manifest.Labels[known.ConfigNamespaceLabel],
			Name:      manifest.Labels[known.ConfigNameLabel],
		}, base.Spec.Feeds) {
			continue
		}

		// prune labels
		// since Base is not referring this resource anymore
		delete(manifest.Labels, string(base.UID))
		delete(manifest.Labels, base.Labels[known.ConfigUIDLabel]) // Subscription
	}
}
