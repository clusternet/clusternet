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

package helmchart

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("HelmChart")

type SyncHandlerFunc func(chart *appsapi.HelmChart) error

// Controller is a controller that handles HelmChart
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient    clusternetclientset.Interface
	helmChartLister     applisters.HelmChartLister
	baseLister          applisters.BaseLister
	baseIndexer         cache.Indexer
	feedInUseProtection bool
	recorder            record.EventRecorder
	syncHandlerFunc     SyncHandlerFunc
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	helmChartInformer appinformers.HelmChartInformer,
	baseInformer appinformers.BaseInformer,
	feedInUseProtection bool,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:    clusternetClient,
		helmChartLister:     helmChartInformer.Lister(),
		baseLister:          baseInformer.Lister(),
		baseIndexer:         baseInformer.Informer().GetIndexer(),
		feedInUseProtection: feedInUseProtection,
		recorder:            recorder,
		syncHandlerFunc:     syncHandlerFunc,
	}
	// create a yacht controller for HelmChart
	yachtController := yacht.NewController("helmChart").
		WithCacheSynced(
			helmChartInformer.Informer().HasSynced,
			baseInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec changes
			if oldObj != nil && newObj != nil {
				oldChart := oldObj.(*appsapi.HelmChart)
				newChart := newObj.(*appsapi.HelmChart)

				if newChart.DeletionTimestamp != nil {
					return true, nil
				}

				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldChart.Spec, newChart.Spec) {
					klog.V(4).Infof("no updates on the spec of HelmChart %s, skipping syncing", klog.KObj(oldChart))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of HelmChart
	_, err := helmChartInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	_, err = baseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldBase := oldObj.(*appsapi.Base)
			newBase := newObj.(*appsapi.Base)

			if newBase.DeletionTimestamp != nil {
				// labels pruning had already been done when a Base got deleted.
				return
			}

			// Decide whether discovery has reported a spec change.
			if reflect.DeepEqual(oldBase.Spec.Feeds, newBase.Spec.Feeds) {
				return
			}

			for _, feed := range utils.FindObsoletedFeeds(oldBase.Spec.Feeds, newBase.Spec.Feeds) {
				if feed.Kind != "HelmChart" {
					continue
				}
				namespacedKey := feed.Namespace + "/" + feed.Name
				klog.V(6).Infof("pruning labels of HelmChart %q", namespacedKey)
				yachtController.Enqueue(namespacedKey)
			}
		},
	})
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
// converge the two. It then updates the Status block of the HelmChart resource
// with the current status of the resource.
func (c *Controller) handle(obj interface{}) (requeueAfter *time.Duration, err error) {
	// If an error occurs during handling, we'll requeue the item so we can
	// attempt processing again later. This could have been caused by a
	// temporary network failure, or any other transient reason.

	// Convert the namespace/name string into a distinct namespace and name
	key := obj.(string)
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing HelmChart %q", key)
	// Get the HelmChart resource with this name
	cachedChart, err := c.helmChartLister.HelmCharts(ns).Get(name)
	// The HelmChart resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("HelmChart %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	chart := cachedChart.DeepCopy()
	if chart.DeletionTimestamp == nil {
		// add finalizer
		if !utils.ContainsString(chart.Finalizers, known.AppFinalizer) {
			chart.Finalizers = append(chart.Finalizers, known.AppFinalizer)
		}
		if !utils.ContainsString(chart.Finalizers, known.FeedProtectionFinalizer) && c.feedInUseProtection {
			chart.Finalizers = append(chart.Finalizers, known.FeedProtectionFinalizer)
		}

		// append Clusternet labels
		if chart.Labels == nil {
			chart.Labels = map[string]string{}
		}
		chart.Labels[known.ConfigGroupLabel] = controllerKind.Group
		chart.Labels[known.ConfigVersionLabel] = controllerKind.Version
		chart.Labels[known.ConfigKindLabel] = controllerKind.Kind
		chart.Labels[known.ConfigNameLabel] = chart.Name
		chart.Labels[known.ConfigNamespaceLabel] = chart.Namespace

		// prune redundant labels
		pruneLabels(chart, c.baseIndexer)

		// only update on changed
		if !reflect.DeepEqual(chart, cachedChart) {
			if chart, err = c.clusternetClient.AppsV1alpha1().HelmCharts(chart.Namespace).Update(context.TODO(),
				chart, metav1.UpdateOptions{}); err != nil {
				msg := fmt.Sprintf("failed to inject finalizers to HelmChart %s: %v", klog.KObj(chart), err)
				klog.WarningDepth(4, msg)
				c.recorder.Event(chart, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
				return nil, err
			}
			msg := fmt.Sprintf("successfully inject finalizers to HelmChart %s", klog.KObj(chart))
			klog.V(4).Info(msg)
			c.recorder.Event(chart, corev1.EventTypeNormal, "FinalizerInjected", msg)
		}
	}

	chart.Kind = controllerKind.Kind
	chart.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(chart)
	if err != nil {
		c.recorder.Event(chart, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(chart, corev1.EventTypeNormal, "Synced", "HelmChart synced successfully")
		klog.Infof("successfully synced HelmChart %q", key)
	}
	return nil, err
}
func (c *Controller) UpdateChartStatus(chartCopy *appsapi.HelmChart, status *appsapi.HelmChartStatus) error {
	klog.V(5).Infof("try to update HelmChart %q status", chartCopy.Name)

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		chartCopy.Status = *status
		_, err := c.clusternetClient.AppsV1alpha1().HelmCharts(chartCopy.Namespace).UpdateStatus(context.TODO(), chartCopy, metav1.UpdateOptions{})
		if err == nil {
			//TODO
			return nil
		}

		updated, err2 := c.helmChartLister.HelmCharts(chartCopy.Namespace).Get(chartCopy.Name)
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			chartCopy = updated.DeepCopy()
			return nil
		}
		utilruntime.HandleError(fmt.Errorf("error getting updated HelmChart %q from lister: %v", chartCopy.Name, err2))
		return err2
	})
}

func pruneLabels(chart *appsapi.HelmChart, baseIndexer cache.Indexer) {
	// find all Bases
	baseUIDs := []string{}
	for key, val := range chart.Labels {
		// normally the length of a uuid is 36
		if len(key) != 36 || strings.Contains(key, "/") || val != "Base" {
			continue
		}
		baseUIDs = append(baseUIDs, key)
	}

	for _, base := range utils.FindBasesFromUIDs(baseIndexer, baseUIDs) {
		if utils.HasFeed(appsapi.Feed{
			Kind:      controllerKind.Kind,
			Namespace: chart.Namespace,
			Name:      chart.Name,
		}, base.Spec.Feeds) {
			continue
		}

		// prune labels
		// since Base is not referring this HelmChart anymore
		delete(chart.Labels, string(base.UID))
		delete(chart.Labels, base.Labels[known.ConfigUIDLabel]) // Subscription
	}
}
