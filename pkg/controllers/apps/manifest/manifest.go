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

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Manifest")

type SyncHandlerFunc func(manifest *appsapi.Manifest) error

// Controller is a controller that handles Manifest
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient    clusternetclientset.Interface
	manifestLister      applisters.ManifestLister
	baseLister          applisters.BaseLister
	baseIndexer         cache.Indexer
	feedInUseProtection bool
	recorder            record.EventRecorder
	syncHandlerFunc     SyncHandlerFunc
	// namespace where Manifests are created
	reservedNamespace string
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	manifestInformer appinformers.ManifestInformer,
	baseInformer appinformers.BaseInformer,
	feedInUseProtection bool,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
	reservedNamespace string,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:    clusternetClient,
		manifestLister:      manifestInformer.Lister(),
		baseLister:          baseInformer.Lister(),
		baseIndexer:         baseInformer.Informer().GetIndexer(),
		feedInUseProtection: feedInUseProtection,
		recorder:            recorder,
		syncHandlerFunc:     syncHandlerFunc,
		reservedNamespace:   reservedNamespace,
	}
	// create a yacht controller for manifest
	yachtController := yacht.NewController("manifest").
		WithCacheSynced(
			manifestInformer.Informer().HasSynced,
			baseInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec and labels changes
			if oldObj != nil && newObj != nil {
				oldManifest := oldObj.(*appsapi.Manifest)
				newManifest := newObj.(*appsapi.Manifest)

				if newManifest.DeletionTimestamp != nil {
					return true, nil
				}

				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldManifest.Template, newManifest.Template) {
					klog.V(4).Infof("no updates on Manifest template %s, skipping syncing", klog.KObj(oldManifest))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of Manifest
	_, err := manifestInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
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

			klog.V(6).Info("waiting for Manifest caches to sync before pruning labels")
			if !cache.WaitForCacheSync(context.TODO().Done(), manifestInformer.Informer().HasSynced) {
				return
			}

			for _, feed := range utils.FindObsoletedFeeds(oldBase.Spec.Feeds, newBase.Spec.Feeds) {
				if feed.Kind == "HelmChart" {
					continue
				}

				manifests, err2 := utils.ListManifestsBySelector(c.reservedNamespace, c.manifestLister, feed)
				if err2 != nil {
					klog.ErrorDepth(6, err2)
					continue
				}

				for _, manifest := range manifests {
					klog.V(6).Infof("pruning labels of Manifest %q", klog.KObj(manifest))
					yachtController.Enqueue(manifest)
				}
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
// converge the two. It then updates the Status block of the Manifest resource
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

	klog.V(4).Infof("start processing Manifest %q", key)
	// Get the Manifest resource with this name
	cachedManifest, err := c.manifestLister.Manifests(ns).Get(name)
	// The Manifest resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Manifest %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	manifest := cachedManifest.DeepCopy()
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
		pruneLabels(updatedManifest, c.baseIndexer)

		// only update on changed
		if !reflect.DeepEqual(manifest, updatedManifest) {
			if manifest, err = c.clusternetClient.AppsV1alpha1().Manifests(updatedManifest.Namespace).Update(context.TODO(),
				updatedManifest, metav1.UpdateOptions{}); err != nil {
				msg := fmt.Sprintf("failed to inject finalizers to Manifest %s: %v", klog.KObj(manifest), err)
				klog.WarningDepth(4, msg)
				c.recorder.Event(manifest, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
				return nil, err
			}
			msg := fmt.Sprintf("successfully inject finalizers to Manifest %s", klog.KObj(manifest))
			klog.V(4).Info(msg)
			c.recorder.Event(manifest, corev1.EventTypeNormal, "FinalizerInjected", msg)
		}
	}

	manifest.Kind = controllerKind.Kind
	manifest.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(manifest)
	if err != nil {
		c.recorder.Event(manifest, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(manifest, corev1.EventTypeNormal, "Synced", "Manifest synced successfully")
		klog.Infof("successfully synced Manifest %q", key)
	}
	return nil, err
}

func pruneLabels(manifest *appsapi.Manifest, baseIndexer cache.Indexer) {
	// find all Bases
	baseUIDs := []string{}
	for key, val := range manifest.Labels {
		// normally the length of a uuid is 36
		if len(key) != 36 || strings.Contains(key, "/") || val != "Base" {
			continue
		}
		baseUIDs = append(baseUIDs, key)
	}

	for _, base := range utils.FindBasesFromUIDs(baseIndexer, baseUIDs) {
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
