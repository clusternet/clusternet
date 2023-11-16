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

package subscription

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
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Subscription")

type SyncHandlerFunc func(subscription *appsapi.Subscription) error

// Controller is a controller that handles Subscription
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetclientset.Interface
	subsLister       applisters.SubscriptionLister
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc
}

// NewController returns a new Controller, which is only responsible for populating Bases.
// The scheduling parts are handled by clusternet-scheduler.
func NewController(
	clusternetClient clusternetclientset.Interface,
	subsInformer appinformers.SubscriptionInformer,
	baseInformer appinformers.BaseInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		subsLister:       subsInformer.Lister(),
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}
	// create a yacht controller for subscription
	yachtController := yacht.NewController("subscription").
		WithCacheSynced(
			subsInformer.Informer().HasSynced,
			baseInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE
			if oldObj != nil && newObj != nil {
				oldSub := oldObj.(*appsapi.Subscription)
				newSub := newObj.(*appsapi.Subscription)

				if newSub.DeletionTimestamp != nil {
					return true, nil
				}

				// Decide whether discovery has reported a status change.
				// clusternet-scheduler is responsible for spec changes.
				if reflect.DeepEqual(oldSub.Status, newSub.Status) {
					klog.V(4).Infof("no updates on the status of Subscription %s, skipping syncing", klog.KObj(oldSub))
					return false, nil
				}
				// no changes on binding namespaces and spec hash
				if reflect.DeepEqual(oldSub.Status.BindingClusters, newSub.Status.BindingClusters) &&
					reflect.DeepEqual(oldSub.Status.Replicas, newSub.Status.Replicas) &&
					oldSub.Status.SpecHash == newSub.Status.SpecHash {
					klog.V(4).Infof("no changes on binding namespaces and spec hash of Subscription %s, skipping syncing", klog.KObj(oldSub))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of Subscription
	_, err := subsInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	_, err = baseInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			base, ok := obj.(*appsapi.Base)
			if !ok {
				tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
					return
				}
				base, ok2 = tombstone.Obj.(*appsapi.Base)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Base %#v", obj))
					return
				}
			}

			sub := c.resolveControllerRef(base.Labels[known.ConfigNameLabel],
				base.Labels[known.ConfigNamespaceLabel], types.UID(base.Labels[known.ConfigUIDLabel]))
			if sub == nil {
				return
			}
			klog.V(4).Infof("deleting Base %q", klog.KObj(base))
			yachtController.Enqueue(sub)
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
func (c *Controller) resolveControllerRef(name, namespace string, uid types.UID) *appsapi.Subscription {
	sub, err := c.subsLister.Subscriptions(namespace).Get(name)
	if err != nil {
		return nil
	}
	if sub.UID != uid {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return sub
}

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Subscription resource
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

	klog.V(4).Infof("start processing Subscription %q", key)
	// Get the Subscription resource with this name
	cachedSub, err := c.subsLister.Subscriptions(ns).Get(name)
	// The Subscription resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Subscription %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// add finalizer
	sub := cachedSub.DeepCopy()
	if !utils.ContainsString(sub.Finalizers, known.AppFinalizer) && sub.DeletionTimestamp == nil {
		sub.Finalizers = append(sub.Finalizers, known.AppFinalizer)
		if sub, err = c.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Update(context.TODO(),
			sub, metav1.UpdateOptions{}); err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Subscription %s: %v", known.AppFinalizer, klog.KObj(sub), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(sub, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Subscription %s", known.AppFinalizer, klog.KObj(sub))
		klog.V(4).Info(msg)
		c.recorder.Event(sub, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	// label Subscription self uid
	if val, ok := sub.Labels[string(sub.UID)]; !ok || val != controllerKind.Kind {
		sub, err = c.patchSubscriptionLabels(sub, map[string]*string{
			string(sub.UID): utilpointer.StringPtr(controllerKind.Kind),
		})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to patch Subscription %s labels: %v", klog.KObj(sub), err))
			return nil, err
		}
	}

	sub.Kind = controllerKind.Kind
	sub.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(sub)
	if err != nil {
		c.recorder.Event(sub, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(sub, corev1.EventTypeNormal, "Synced", "Subscription synced successfully")
		klog.Infof("successfully synced Subscription %q", key)
	}
	return nil, err
}

func (c *Controller) patchSubscriptionLabels(sub *appsapi.Subscription, labels map[string]*string) (*appsapi.Subscription, error) {
	if sub.DeletionTimestamp != nil || len(labels) == 0 {
		return sub, nil
	}

	klog.V(5).Infof("patching Subscription %s labels", klog.KObj(sub))
	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := utils.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Subscriptions(sub.Namespace).Patch(context.TODO(),
		sub.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}
