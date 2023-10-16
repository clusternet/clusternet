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

package helmrelease

import (
	"context"
	"fmt"
	"reflect"
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
var controllerKind = appsapi.SchemeGroupVersion.WithKind("HelmRelease")

type SyncHandlerFunc func(helmrelease *appsapi.HelmRelease) error

// Controller is a controller that handles HelmRelease
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetclientset.Interface
	hrLister         applisters.HelmReleaseLister
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	hrInformer appinformers.HelmReleaseInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		hrLister:         hrInformer.Lister(),
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}
	// create a yacht controller for HelmRelease
	yachtController := yacht.NewController("helmRelease").
		WithCacheSynced(
			hrInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec changes
			if oldObj != nil && newObj != nil {
				oldHr := oldObj.(*appsapi.HelmRelease)
				newHr := newObj.(*appsapi.HelmRelease)

				if newHr.DeletionTimestamp != nil {
					return true, nil
				}

				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldHr.Spec, newHr.Spec) {
					klog.V(4).Infof("no updates on the spec of HelmRelease %s, skipping syncing", klog.KObj(oldHr))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of HelmRelease
	_, err := hrInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
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
// converge the two. It then updates the Status block of the HelmRelease resource
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

	klog.V(4).Infof("start processing HelmRelease %q", key)
	// Get the HelmRelease resource with this name
	cachedhr, err := c.hrLister.HelmReleases(ns).Get(name)
	// The HelmRelease resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("HelmRelease %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	hr := cachedhr.DeepCopy()
	// add finalizer
	if !utils.ContainsString(hr.Finalizers, known.AppFinalizer) && hr.DeletionTimestamp == nil {
		hr.Finalizers = append(hr.Finalizers, known.AppFinalizer)
		hr, err = c.clusternetClient.AppsV1alpha1().HelmReleases(hr.Namespace).Update(context.TODO(),
			hr, metav1.UpdateOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to HelmRelease %s: %v",
				known.AppFinalizer, klog.KObj(hr), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(hr, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to HelmRelease %s",
			known.AppFinalizer, klog.KObj(hr))
		klog.V(4).Info(msg)
		c.recorder.Event(hr, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	hr.Kind = controllerKind.Kind
	hr.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(hr)
	if err != nil {
		c.recorder.Event(hr, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(hr, corev1.EventTypeNormal, "Synced", "HelmRelease synced successfully")
		klog.Infof("successfully synced HelmRelease %q", key)
	}
	return nil, err
}
