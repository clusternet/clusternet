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

package localization

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

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var (
	controllerKind = appsapi.SchemeGroupVersion.WithKind("Localization")
	chartKind      = appsapi.SchemeGroupVersion.WithKind("HelmChart")
)

type SyncHandlerFunc func(loc *appsapi.Localization) error

// Controller is a controller that handles Localization
type Controller struct {
	clusternetClient clusternetclientset.Interface

	// workqueue is a rate limited work queue. This is used to queue work to be
	// processed instead of performing it as soon as a change happens. This
	// means we can ensure we only process a fixed amount of resources at a
	// time, and makes it easy to ensure we are never processing the same item
	// simultaneously in two different workers.
	workqueue workqueue.RateLimitingInterface

	locLister applisters.LocalizationLister
	locSynced cache.InformerSynced

	chartLister applisters.HelmChartLister
	chartSynced cache.InformerSynced

	manifestLister applisters.ManifestLister
	manifestSynced cache.InformerSynced

	recorder record.EventRecorder

	syncHandlerFunc SyncHandlerFunc

	// namespace where Manifests are created
	reservedNamespace string
}

func NewController(clusternetClient clusternetclientset.Interface,
	locInformer appinformers.LocalizationInformer,
	chartInformer appinformers.HelmChartInformer, manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc, reservedNamespace string) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		clusternetClient:  clusternetClient,
		workqueue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "Localization"),
		locLister:         locInformer.Lister(),
		locSynced:         locInformer.Informer().HasSynced,
		chartLister:       chartInformer.Lister(),
		chartSynced:       chartInformer.Informer().HasSynced,
		manifestLister:    manifestInformer.Lister(),
		manifestSynced:    manifestInformer.Informer().HasSynced,
		recorder:          recorder,
		syncHandlerFunc:   syncHandlerFunc,
		reservedNamespace: reservedNamespace,
	}

	// Manage the addition/update of Localization
	_, err := locInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addLocalization,
		UpdateFunc: c.updateLocalization,
		DeleteFunc: c.deleteLocalization,
	})
	if err != nil {
		return nil, err
	}

	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	klog.Info("starting localization controller...")
	defer klog.Info("shutting down localization controller")

	// Wait for the caches to be synced before starting workers
	if !cache.WaitForNamedCacheSync("localization-controller", stopCh, c.locSynced, c.chartSynced, c.manifestSynced) {
		return
	}

	klog.V(5).Infof("starting %d worker threads", workers)
	// Launch workers to process Localization resources
	for i := 0; i < workers; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	<-stopCh
}

func (c *Controller) addLocalization(obj interface{}) {
	loc := obj.(*appsapi.Localization)
	klog.V(4).Infof("adding Localization %q", klog.KObj(loc))
	c.enqueue(loc)
}

func (c *Controller) updateLocalization(old, cur interface{}) {
	oldLoc := old.(*appsapi.Localization)
	newLoc := cur.(*appsapi.Localization)

	if newLoc.DeletionTimestamp != nil {
		c.enqueue(newLoc)
		return
	}

	if reflect.DeepEqual(oldLoc.Labels, newLoc.Labels) && reflect.DeepEqual(oldLoc.Spec, newLoc.Spec) {
		// Decide whether discovery has reported labels or spec change.
		klog.V(4).Infof("no updates on the labels and spec of Localization %s, skipping syncing", klog.KObj(oldLoc))
		return
	}

	klog.V(4).Infof("updating Localization %q", klog.KObj(oldLoc))
	c.enqueue(newLoc)
}

func (c *Controller) deleteLocalization(obj interface{}) {
	loc, ok := obj.(*appsapi.Localization)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
			return
		}
		loc, ok = tombstone.Obj.(*appsapi.Localization)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Localization %#v", obj))
			return
		}
	}
	klog.V(4).Infof("deleting Localization %q", klog.KObj(loc))
	c.enqueue(loc)
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
		// Localization resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("successfully synced Localization %q", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Localization resource
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

	klog.V(4).Infof("start processing Localization %q", key)
	// Get the Localization resource with this name
	cachedLoc, err := c.locLister.Localizations(ns).Get(name)
	// The Localization resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Localization %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	loc := cachedLoc.DeepCopy()
	// add finalizer
	if !utils.ContainsString(loc.Finalizers, known.AppFinalizer) && loc.DeletionTimestamp == nil {
		loc.Finalizers = append(loc.Finalizers, known.AppFinalizer)
		loc, err = c.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Update(context.TODO(),
			loc, metav1.UpdateOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Localization %s: %v",
				known.AppFinalizer, klog.KObj(loc), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(loc, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Localization %s",
			known.AppFinalizer, klog.KObj(loc))
		klog.V(4).Info(msg)
		c.recorder.Event(loc, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	// label matching Feeds uid to Localization
	labelsToPatch, err := c.getLabelsForPatching(loc)
	if err == nil {
		loc, err = c.patchLocalizationLabels(loc, labelsToPatch)
	}
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("failed to patch labels to Localization %s: %v",
			klog.KObj(loc), err))
		return err
	}

	loc.Kind = controllerKind.Kind
	loc.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(loc)
	if err != nil {
		c.recorder.Event(loc, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(loc, corev1.EventTypeNormal, "Synced", "Localization synced successfully")
	}
	return err
}

// enqueue takes a Localization resource and converts it into a namespace/name
// string which is then put onto the work queue. This method should *not* be
// passed resources of any type other than Localization.
func (c *Controller) enqueue(loc *appsapi.Localization) {
	key, err := cache.MetaNamespaceKeyFunc(loc)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	c.workqueue.Add(key)
}

func (c *Controller) getLabelsForPatching(loc *appsapi.Localization) (map[string]*string, error) {
	labelsToPatch := map[string]*string{}
	if loc.DeletionTimestamp != nil {
		return labelsToPatch, nil
	}
	switch loc.Spec.Feed.Kind {
	case chartKind.Kind:
		chart, err := c.chartLister.HelmCharts(loc.Spec.Feed.Namespace).Get(loc.Spec.Feed.Name)
		if err != nil {
			return nil, err
		}
		val, ok := loc.Labels[string(chart.UID)]
		if !ok || val != chartKind.Kind {
			labelsToPatch[string(chart.UID)] = &chartKind.Kind
		}
	default:
		manifests, err := utils.ListManifestsBySelector(c.reservedNamespace, c.manifestLister, loc.Spec.Feed)
		if err != nil {
			return nil, err
		}
		if manifests == nil {
			return nil, fmt.Errorf("%s is not found for Localization %s", utils.FormatFeed(loc.Spec.Feed), klog.KObj(loc))
		}
		for _, manifest := range manifests {
			kind := manifest.Labels[known.ConfigKindLabel]
			val, ok := loc.Labels[string(manifest.UID)]
			if !ok || val != kind {
				labelsToPatch[string(manifest.UID)] = &kind
			}
		}
	}

	return labelsToPatch, nil
}

func (c *Controller) patchLocalizationLabels(loc *appsapi.Localization, labels map[string]*string) (*appsapi.Localization, error) {
	if loc.DeletionTimestamp != nil || len(labels) == 0 {
		return loc, nil
	}

	klog.V(5).Infof("patching Localization %s labels", klog.KObj(loc))
	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := json.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Patch(context.TODO(),
		loc.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}
