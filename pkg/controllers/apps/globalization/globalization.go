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

package globalization

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
	controllerKind = appsapi.SchemeGroupVersion.WithKind("Globalization")
	chartKind      = appsapi.SchemeGroupVersion.WithKind("HelmChart")
)

type SyncHandlerFunc func(glob *appsapi.Globalization) error

// Controller is a controller that handles Globalization
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetclientset.Interface
	globLister       applisters.GlobalizationLister
	chartLister      applisters.HelmChartLister
	manifestLister   applisters.ManifestLister
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc
	// namespace where Manifests are created
	reservedNamespace string
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	globInformer appinformers.GlobalizationInformer,
	chartInformer appinformers.HelmChartInformer,
	manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
	reservedNamespace string,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:  clusternetClient,
		globLister:        globInformer.Lister(),
		chartLister:       chartInformer.Lister(),
		manifestLister:    manifestInformer.Lister(),
		recorder:          recorder,
		syncHandlerFunc:   syncHandlerFunc,
		reservedNamespace: reservedNamespace,
	}
	// create a yacht controller for globalization
	yachtController := yacht.NewController("globalization").
		WithCacheSynced(
			globInformer.Informer().HasSynced,
			chartInformer.Informer().HasSynced,
			manifestInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: labels and spec changes
			if oldObj != nil && newObj != nil {
				oldGlob := oldObj.(*appsapi.Globalization)
				newGlob := newObj.(*appsapi.Globalization)
				if newGlob.DeletionTimestamp != nil {
					return true, nil
				}

				if reflect.DeepEqual(oldGlob.Labels, newGlob.Labels) && reflect.DeepEqual(oldGlob.Spec, newGlob.Spec) {
					// Decide whether discovery has reported labels or spec change.
					klog.V(4).Infof("no updates on the labels and spec of Globalization %s, skipping syncing", klog.KObj(oldGlob))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of Globalization
	_, err := globInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
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
// converge the two. It then updates the Status block of the Globalization resource
// with the current status of the resource.
func (c *Controller) handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	key := obj.(string)
	_, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing Globalization %q", key)
	// Get the Globalization resource with this name
	cachedGlob, err := c.globLister.Get(name)
	// The Globalization resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Globalization %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// add finalizer
	glob := cachedGlob.DeepCopy()
	if !utils.ContainsString(glob.Finalizers, known.AppFinalizer) && glob.DeletionTimestamp == nil {
		glob.Finalizers = append(glob.Finalizers, known.AppFinalizer)
		glob, err = c.clusternetClient.AppsV1alpha1().Globalizations().Update(context.TODO(),
			glob, metav1.UpdateOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Globalization %s: %v",
				known.AppFinalizer, klog.KObj(glob), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(glob, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Globalization %s",
			known.AppFinalizer, klog.KObj(glob))
		klog.V(4).Info(msg)
		c.recorder.Event(glob, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	// label matching Feeds uid to Globalization
	labelsToPatch, err := c.getLabelsForPatching(glob)
	if err == nil {
		glob, err = c.patchGlobalizationLabels(glob, labelsToPatch)
	}
	if err != nil {
		klog.ErrorDepth(5, fmt.Sprintf("failed to patch labels to Globalization %s: %v",
			klog.KObj(glob), err))
		return nil, err
	}

	glob.Kind = controllerKind.Kind
	glob.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(glob)
	if err != nil {
		c.recorder.Event(glob, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(glob, corev1.EventTypeNormal, "Synced", "Globalization synced successfully")
		klog.Infof("successfully synced Globalization %q", key)
	}
	return nil, err
}

func (c *Controller) getLabelsForPatching(glob *appsapi.Globalization) (map[string]*string, error) {
	labelsToPatch := map[string]*string{}
	if glob.DeletionTimestamp != nil {
		return labelsToPatch, nil
	}
	switch glob.Spec.Feed.Kind {
	case chartKind.Kind:
		chart, err := c.chartLister.HelmCharts(glob.Spec.Feed.Namespace).Get(glob.Spec.Feed.Name)
		if err != nil {
			return nil, err
		}
		val, ok := glob.Labels[string(chart.UID)]
		if !ok || val != chartKind.Kind {
			labelsToPatch[string(chart.UID)] = &chartKind.Kind
		}
	default:
		manifests, err := utils.ListManifestsBySelector(c.reservedNamespace, c.manifestLister, glob.Spec.Feed)
		if err != nil {
			return nil, err
		}
		if manifests == nil {
			return nil, fmt.Errorf("%s is not found for Globalization %s", utils.FormatFeed(glob.Spec.Feed), klog.KObj(glob))
		}
		for _, manifest := range manifests {
			kind := manifest.Labels[known.ConfigKindLabel]
			val, ok := glob.Labels[string(manifest.UID)]
			if !ok || val != kind {
				labelsToPatch[string(manifest.UID)] = &kind
			}
		}

	}
	return labelsToPatch, nil
}

func (c *Controller) patchGlobalizationLabels(glob *appsapi.Globalization, labels map[string]*string) (*appsapi.Globalization, error) {
	if glob.DeletionTimestamp != nil || len(labels) == 0 {
		return glob, nil
	}

	klog.V(5).Infof("patching Globalization %s labels", klog.KObj(glob))
	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := utils.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Globalizations().Patch(context.TODO(),
		glob.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}
