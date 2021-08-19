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

// Controller is a controller that handle Localization
type Controller struct {
	ctx context.Context

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
}

func NewController(ctx context.Context, clusternetClient clusternetclientset.Interface,
	locInformer appinformers.LocalizationInformer,
	chartInformer appinformers.HelmChartInformer, manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder, syncHandlerFunc SyncHandlerFunc) (*Controller, error) {
	if syncHandlerFunc == nil {
		return nil, fmt.Errorf("syncHandlerFunc must be set")
	}

	c := &Controller{
		ctx:              ctx,
		clusternetClient: clusternetClient,
		workqueue:        workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "localization"),
		locLister:        locInformer.Lister(),
		locSynced:        locInformer.Informer().HasSynced,
		chartLister:      chartInformer.Lister(),
		chartSynced:      chartInformer.Informer().HasSynced,
		manifestLister:   manifestInformer.Lister(),
		manifestSynced:   manifestInformer.Informer().HasSynced,
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}

	// Manage the addition/update of Localization
	locInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    c.addLocalization,
		UpdateFunc: c.updateLocalization,
		DeleteFunc: c.deleteLocalization,
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

	klog.Info("starting localization controller...")
	defer klog.Info("shutting down localization controller")

	// Wait for the caches to be synced before starting workers
	klog.V(5).Info("waiting for informer caches to sync")
	if !cache.WaitForCacheSync(stopCh, c.locSynced, c.chartSynced, c.manifestSynced) {
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

	// add finalizer
	if !utils.ContainsString(loc.Finalizers, known.AppFinalizer) && loc.DeletionTimestamp == nil {
		loc.Finalizers = append(loc.Finalizers, known.AppFinalizer)
		_, err := c.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Update(context.TODO(),
			loc, metav1.UpdateOptions{})
		if err == nil {
			msg := fmt.Sprintf("successfully inject finalizer %s to Localization %s",
				known.AppFinalizer, klog.KObj(loc))
			klog.V(4).Info(msg)
			c.recorder.Event(loc, corev1.EventTypeNormal, "FinalizerInjected", msg)
		} else {
			msg := fmt.Sprintf("failed to inject finalizer %s to Localization %s: %v",
				known.AppFinalizer, klog.KObj(loc), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(loc, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			c.addLocalization(obj)
			return
		}
	}

	// label matching Feeds uid to Localization
	labelsToPatch, err := c.getLabelsForPatching(loc)
	if err == nil {
		err = c.PatchLocalizationLabels(loc, labelsToPatch)
	}
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("failed to patch labels to Globalization %s: %v",
			klog.KObj(loc), err))
		c.addLocalization(obj)
		return
	}

	c.enqueue(loc)
}

func (c *Controller) updateLocalization(old, cur interface{}) {
	oldLoc := old.(*appsapi.Localization)
	newLoc := cur.(*appsapi.Localization)

	if newLoc.DeletionTimestamp != nil {
		c.enqueue(newLoc)
		return
	}

	// label matching Feeds uid to Globalization
	labelsToPatch, err := c.getLabelsForPatching(newLoc)
	if err == nil {
		err = c.PatchLocalizationLabels(newLoc, labelsToPatch)
	}
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("failed to patch labels to Globalization %s: %v",
			klog.KObj(newLoc), err))
		c.updateLocalization(old, cur)
		return
	}

	// Decide whether discovery has reported a spec change.
	if reflect.DeepEqual(oldLoc.Spec, newLoc.Spec) {
		klog.V(4).Infof("no updates on the spec of Localization %q, skipping syncing", oldLoc.Name)
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
	loc, err := c.locLister.Localizations(ns).Get(name)
	// The Localization resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Localization %q has been deleted", key)
		return nil
	}
	if err != nil {
		return err
	}

	loc.Kind = controllerKind.Kind
	loc.APIVersion = controllerKind.Version

	return c.syncHandlerFunc(loc)
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
	if loc.Spec.Feed.Kind == chartKind.Kind {
		charts, err := utils.ListChartsBySelector(c.chartLister, loc.Spec.Feed)
		if err != nil {
			return nil, err
		}
		for _, chart := range charts {
			val, ok := loc.Labels[string(chart.UID)]
			if !ok || val != chartKind.Kind {
				labelsToPatch[string(chart.UID)] = &chartKind.Kind
			}
		}
	} else {
		manifests, err := utils.ListManifestsBySelector(c.manifestLister, loc.Spec.Feed)
		if err != nil {
			return nil, err
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

func (c *Controller) PatchLocalizationLabels(loc *appsapi.Localization, labels map[string]*string) error {
	klog.V(5).Infof("patching Localization labels")
	if len(labels) == 0 {
		return nil
	}

	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := json.Marshal(option)
	if err != nil {
		return err
	}

	_, err = c.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Patch(context.TODO(),
		loc.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
	return err
}
