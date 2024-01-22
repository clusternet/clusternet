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

package description

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	appListers "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Description")

type SyncHandlerFunc func(description *appsapi.Description) error

// Controller is a controller that handles Description
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetClientSet.Interface
	descLister       appListers.DescriptionLister
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc
}

func NewController(
	clusternetClient clusternetClientSet.Interface,
	descInformer appInformers.DescriptionInformer,
	hrInformer appInformers.HelmReleaseInformer,
	helmChartInformer appInformers.HelmChartInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		descLister:       descInformer.Lister(),
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}
	// create a yacht controller for description
	yachtController := yacht.NewController("description").
		WithCacheSynced(
			descInformer.Informer().HasSynced,
			hrInformer.Informer().HasSynced,
			helmChartInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec changes
			if oldObj != nil && newObj != nil {
				oldDesc := oldObj.(*appsapi.Description)
				newDesc := newObj.(*appsapi.Description)
				if newDesc.DeletionTimestamp != nil {
					return true, nil
				}
				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldDesc.Spec, newDesc.Spec) {
					klog.V(4).Infof("no updates on the spec of Description %s, skipping syncing", klog.KObj(oldDesc))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of Description
	_, err := descInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	_, err = hrInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			hr, ok := obj.(*appsapi.HelmRelease)
			if !ok {
				tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
					return
				}
				hr, ok2 = tombstone.Obj.(*appsapi.HelmRelease)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a HelmRelease %#v", obj))
					return
				}
			}

			controllerRef := metav1.GetControllerOf(hr)
			if controllerRef == nil {
				// No controller should care about orphans being deleted.
				return
			}
			desc := c.resolveControllerRef(hr.Namespace, controllerRef)
			if desc == nil {
				return
			}
			klog.V(4).Infof("deleting HelmRelease %q", klog.KObj(hr))
			yachtController.Enqueue(desc)
		},
	})
	if err != nil {
		return nil, err
	}

	_, err = helmChartInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldChart := oldObj.(*appsapi.HelmChart)
			newChart := newObj.(*appsapi.HelmChart)

			// Decide whether discovery has reported a spec change.
			if reflect.DeepEqual(oldChart.Spec, newChart.Spec) {
				return
			}

			baseUIDs := []string{}
			for k, v := range newChart.GetLabels() {
				if v == "Base" {
					baseUIDs = append(baseUIDs, k)
				}
			}
			if len(baseUIDs) == 0 {
				return
			}

			klog.V(5).Infof("HelmChart %s/%s has bases: %s", newChart.Namespace, newChart.Name, baseUIDs)
			requirement, err2 := labels.NewRequirement(known.ConfigUIDLabel, selection.In, baseUIDs)
			if err2 != nil {
				utilruntime.HandleError(fmt.Errorf("failed to create label requirement: %v", err2))
				return
			}
			selector := labels.NewSelector().Add(*requirement)
			descs, err3 := c.descLister.Descriptions("").List(selector)
			if err3 != nil {
				utilruntime.HandleError(fmt.Errorf("failed to list descriptions: %v", err3))
				return
			}
			for _, desc := range descs {
				klog.V(4).Infof("updating Description %q", klog.KObj(desc))
				yachtController.Enqueue(desc)
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

// resolveControllerRef returns the controller referenced by a ControllerRef,
// or nil if the ControllerRef could not be resolved to a matching controller
// of the correct Kind.
func (c *Controller) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *appsapi.Description {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != controllerKind.Kind {
		return nil
	}
	desc, err := c.descLister.Descriptions(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if desc.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return desc
}

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Description resource
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

	klog.V(4).Infof("start processing Description %q", key)
	// Get the Description resource with this name
	cachedDesc, err := c.descLister.Descriptions(ns).Get(name)
	// The Description resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Description %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// add finalizer
	desc := cachedDesc.DeepCopy()
	if !utils.ContainsString(desc.Finalizers, known.AppFinalizer) && desc.DeletionTimestamp == nil {
		desc.Finalizers = append(desc.Finalizers, known.AppFinalizer)
		desc, err = c.clusternetClient.AppsV1alpha1().Descriptions(desc.Namespace).Update(context.TODO(),
			desc, metav1.UpdateOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Description %s: %v", known.AppFinalizer, klog.KObj(desc), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(desc, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Description %s", known.AppFinalizer, klog.KObj(desc))
		klog.V(4).Info(msg)
		c.recorder.Event(desc, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	desc.Kind = controllerKind.Kind
	desc.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(desc)
	if err != nil {
		c.recorder.Event(desc, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(desc, corev1.EventTypeNormal, "Synced", "Description synced successfully")
		klog.Infof("successfully synced Description %q", key)
	}
	return nil, err
}
