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

package feedinventory

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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

var subKind = appsapi.SchemeGroupVersion.WithKind("Subscription")

// SyncHandlerFunc is the function to sync a Subscription object
type SyncHandlerFunc func(subscription *appsapi.Subscription) error

// Controller is a controller that maintains FeedInventory objects
type Controller struct {
	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	subLister      applisters.SubscriptionLister
	subSynced      cache.InformerSynced
	finvLister     applisters.FeedInventoryLister
	finvSynced     cache.InformerSynced
	manifestLister applisters.ManifestLister
	manifestSynced cache.InformerSynced

	recorder record.EventRecorder
	registry Registry

	// namespace where Manifests are created
	reservedNamespace     string
	customSyncHandlerFunc SyncHandlerFunc
}

func NewController(clusternetClient clusternetclientset.Interface,
	subsInformer appinformers.SubscriptionInformer,
	finvInformer appinformers.FeedInventoryInformer,
	manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder, registry Registry,
	reservedNamespace string, customSyncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	c := &Controller{
		clusternetClient:      clusternetClient,
		workqueue:             workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "FeedInventory"),
		subLister:             subsInformer.Lister(),
		subSynced:             subsInformer.Informer().HasSynced,
		finvLister:            finvInformer.Lister(),
		finvSynced:            finvInformer.Informer().HasSynced,
		manifestLister:        manifestInformer.Lister(),
		manifestSynced:        manifestInformer.Informer().HasSynced,
		recorder:              recorder,
		registry:              registry,
		reservedNamespace:     reservedNamespace,
		customSyncHandlerFunc: customSyncHandlerFunc,
	}

	_, err := subsInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addSubscription,
		UpdateFunc: c.updateSubscription,
	})
	if err != nil {
		return nil, err
	}

	_, err = finvInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: c.deleteFeedInventory,
	})
	if err != nil {
		return nil, err
	}

	// When a resource gets scaled, such as Deployment,
	// re-enqueue all referring Subscription objects
	_, err = manifestInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addManifest,
		UpdateFunc: c.updateManifest,
	})
	if err != nil {
		return nil, err
	}

	// TODO: helm charts ?

	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("starting feedInventory controller...")
	defer klog.Info("shutting down feedInventory controller")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("feedInventory-controller", stopCh,
		c.subSynced, c.finvSynced, c.manifestSynced) {
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
	klog.V(4).Infof("[FeedInventory] adding Subscription %q", klog.KObj(sub))
	c.enqueue(sub)
}

func (c *Controller) updateSubscription(old, cur interface{}) {
	oldSub := old.(*appsapi.Subscription)
	newSub := cur.(*appsapi.Subscription)

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldSub.Spec, newSub.Spec) {
		klog.V(4).Infof("[FeedInventory] no updates on the spec of Subscription %s, skipping syncing", klog.KObj(newSub))
		return
	}

	klog.V(4).Infof("[FeedInventory] updating Subscription %q", klog.KObj(newSub))
	c.enqueue(newSub)
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

// enqueueManifest takes a Manifest resource and converts it into a namespace/name
// string which is then put onto the work queue.
func (c *Controller) enqueueManifest(manifest *appsapi.Manifest) {
	if manifest.DeletionTimestamp != nil {
		return
	}

	subUIDs := []string{}
	for k, v := range manifest.GetLabels() {
		if v == subKind.Kind {
			subUIDs = append(subUIDs, k)
		}
	}

	allSubs := []*appsapi.Subscription{}
	for _, subUID := range subUIDs {
		subs, err := c.subLister.List(labels.SelectorFromSet(labels.Set{
			subUID: subKind.Kind,
		}))
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		allSubs = append(allSubs, subs...)
	}

	for _, sub := range allSubs {
		c.enqueue(sub)
	}
}

func (c *Controller) addManifest(obj interface{}) {
	manifest := obj.(*appsapi.Manifest)
	klog.V(4).Infof("[FeedInventory] adding Manifest %q", klog.KObj(manifest))
	c.enqueueManifest(manifest)
}

func (c *Controller) updateManifest(old, cur interface{}) {
	oldManifest := old.(*appsapi.Manifest)
	newManifest := cur.(*appsapi.Manifest)

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldManifest.Template, newManifest.Template) {
		klog.V(4).Infof("[FeedInventory] no updates on Manifest template %s, skipping syncing", klog.KObj(oldManifest))
		return
	}

	klog.V(4).Infof("[FeedInventory] updating Manifest %q", klog.KObj(oldManifest))
	c.enqueueManifest(newManifest)
}

func (c *Controller) deleteFeedInventory(obj interface{}) {
	finv := obj.(*appsapi.FeedInventory)
	klog.V(4).Infof("[FeedInventory] deleting FeedInventory %q", klog.KObj(finv))
	c.workqueue.Add(klog.KObj(finv))
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
		klog.Infof("[FeedInventory] successfully synced Subscription %q", key)
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

	klog.V(4).Infof("[FeedInventory] start processing Subscription %q", key)
	// Get the Subscription resource with this name
	sub, err := c.subLister.Subscriptions(ns).Get(name)
	// The Subscription resource may no longer exist, in which case we stop processing.
	if apierrors.IsNotFound(err) {
		klog.V(2).Infof("Subscription %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	if sub.DeletionTimestamp != nil {
		return nil
	}

	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return nil
	}

	sub.Kind = subKind.Kind
	sub.APIVersion = subKind.GroupVersion().String()
	if c.customSyncHandlerFunc != nil {
		return c.customSyncHandlerFunc(sub)
	}
	return c.handleSubscription(sub)
}

func (c *Controller) handleSubscription(sub *appsapi.Subscription) error {
	finv := &appsapi.FeedInventory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sub.Name,
			Namespace: sub.Namespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetHubName,
				// add subscription info
				known.ConfigSubscriptionNameLabel:      sub.Name,
				known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
				known.ConfigSubscriptionUIDLabel:       string(sub.UID),
			},
		},
		Spec: appsapi.FeedInventorySpec{
			Feeds: make([]appsapi.FeedOrder, len(sub.Spec.Feeds)),
		},
	}
	finv.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(sub, subKind)})

	wg := sync.WaitGroup{}
	wg.Add(len(sub.Spec.Feeds))
	errCh := make(chan error, len(sub.Spec.Feeds))
	for idx, feed := range sub.Spec.Feeds {
		go func(idx int, feed appsapi.Feed) {
			defer wg.Done()

			manifests, err2 := utils.ListManifestsBySelector(c.reservedNamespace, c.manifestLister, feed)
			if err2 != nil {
				errCh <- err2
				return
			}
			if manifests == nil {
				errCh <- apierrors.NewNotFound(schema.GroupResource{Resource: feed.Kind}, feed.Name)
				return
			}

			gvk, err2 := getGroupVersionKind(manifests[0].Template.Raw)
			if err2 != nil {
				errCh <- err2
				return
			}

			var desiredReplicas *int32
			var replicaRequirements appsapi.ReplicaRequirements
			var replicaJsonPath string
			plugin, ok := c.registry[gvk]
			if ok {
				klog.V(7).Infof("using plugin %q to parse %q", plugin.Name(), plugin.Kind())
				desiredReplicas, replicaRequirements, replicaJsonPath, err2 = plugin.Parser(manifests[0].Template.Raw)
				if err2 != nil {
					errCh <- err2
					return
				}
			}

			finv.Spec.Feeds[idx] = appsapi.FeedOrder{
				Feed:                feed,
				DesiredReplicas:     desiredReplicas,
				ReplicaRequirements: replicaRequirements,
				ReplicaJsonPath:     replicaJsonPath,
			}
		}(idx, feed)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err3 := range errCh {
		allErrs = append(allErrs, err3)
	}

	var err error
	if len(allErrs) != 0 {
		err = utilerrors.NewAggregate(allErrs)
		c.recorder.Event(sub, corev1.EventTypeWarning, "UnParseableFeeds", err.Error())
		return err
	}

	_, err = c.clusternetClient.AppsV1alpha1().FeedInventories(finv.Namespace).Create(context.TODO(), finv, metav1.CreateOptions{})
	if err != nil && apierrors.IsAlreadyExists(err) {
		curFinv, err2 := c.finvLister.FeedInventories(finv.Namespace).Get(finv.Name)
		if err2 != nil {
			return err2
		}
		finv.SetResourceVersion(curFinv.GetResourceVersion())
		_, err = c.clusternetClient.AppsV1alpha1().FeedInventories(finv.Namespace).Update(context.TODO(), finv, metav1.UpdateOptions{})
	}

	if err != nil {
		c.recorder.Event(sub, corev1.EventTypeWarning, "FeedInventoryNotSynced", err.Error())
	} else {
		c.recorder.Event(sub, corev1.EventTypeNormal, "FeedInventorySynced", "FeedInventory synced successfully")
	}
	return err
}

func getGroupVersionKind(rawData []byte) (schema.GroupVersionKind, error) {
	object := &unstructured.Unstructured{}
	if err := json.Unmarshal(rawData, object); err != nil {
		return schema.GroupVersionKind{}, err
	}

	groupVersion, err := schema.ParseGroupVersion(object.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return groupVersion.WithKind(object.GetKind()), nil
}
