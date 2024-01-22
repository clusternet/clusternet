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
	"fmt"
	"reflect"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
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
	yachtController *yacht.Controller

	locLister      applisters.LocalizationLister
	chartLister    applisters.HelmChartLister
	manifestLister applisters.ManifestLister

	clusternetClient clusternetclientset.Interface
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc

	// namespace where Manifests are created
	reservedNamespace string
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	locInformer appinformers.LocalizationInformer,
	chartInformer appinformers.HelmChartInformer,
	manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
	reservedNamespace string,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:  clusternetClient,
		locLister:         locInformer.Lister(),
		chartLister:       chartInformer.Lister(),
		manifestLister:    manifestInformer.Lister(),
		recorder:          recorder,
		syncHandlerFunc:   syncHandlerFunc,
		reservedNamespace: reservedNamespace,
	}

	// create a yacht controller for localization
	yachtController := yacht.NewController("localization").
		WithCacheSynced(
			locInformer.Informer().HasSynced,
			chartInformer.Informer().HasSynced,
			manifestInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec and labels changes
			if oldObj != nil && newObj != nil {
				oldLoc := oldObj.(*appsapi.Localization)
				newLoc := newObj.(*appsapi.Localization)
				if newLoc.GetDeletionTimestamp() != nil {
					return true, nil
				}
				if reflect.DeepEqual(oldLoc.Labels, newLoc.Labels) && reflect.DeepEqual(oldLoc.Spec, newLoc.Spec) {
					// Decide whether discovery has reported labels or spec change.
					klog.V(4).Infof("no updates on the labels and spec of Localization %s, skipping syncing", klog.KObj(oldLoc))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of Localization
	_, err := locInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	c.yachtController = yachtController
	return c, nil
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(workers int, ctx context.Context) {
	c.yachtController.WithWorkers(workers).Run(ctx)
}

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Localization resource
// with the current status of the resource.
func (c *Controller) handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	// Convert the namespace/name string into a distinct namespace and name
	key := obj.(string)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing Localization %q", key)
	// Get the Localization resource with this name
	cachedLoc, err := c.locLister.Localizations(ns).Get(name)
	// The Localization resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Localization %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
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
			return nil, err
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
		return nil, err
	}

	loc.Kind = controllerKind.Kind
	loc.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(loc)
	if err != nil {
		c.recorder.Event(loc, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(loc, corev1.EventTypeNormal, "Synced", "Localization synced successfully")
		klog.Infof("successfully synced Localization %q", key)
	}
	return nil, err
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
	patchData, err := utils.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Localizations(loc.Namespace).Patch(context.TODO(),
		loc.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}
