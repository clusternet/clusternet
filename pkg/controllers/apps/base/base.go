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

package base

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
var controllerKind = appsapi.SchemeGroupVersion.WithKind("Base")

type SyncHandlerFunc func(base *appsapi.Base) error

// Controller is a controller that handles Base
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetclientset.Interface
	baseLister       applisters.BaseLister
	baseIndexer      cache.Indexer
	recorder         record.EventRecorder
	syncHandlerFunc  SyncHandlerFunc
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	baseInformer appinformers.BaseInformer,
	descInformer appinformers.DescriptionInformer,
	recorder record.EventRecorder,
	syncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		baseLister:       baseInformer.Lister(),
		baseIndexer:      baseInformer.Informer().GetIndexer(),
		recorder:         recorder,
		syncHandlerFunc:  syncHandlerFunc,
	}
	// create a yacht controller for Base
	yachtController := yacht.NewController("base").
		WithCacheSynced(
			baseInformer.Informer().HasSynced,
			descInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: spec and labels changes
			if oldObj != nil && newObj != nil {
				oldBase := oldObj.(*appsapi.Base)
				newBase := newObj.(*appsapi.Base)
				if newBase.DeletionTimestamp != nil {
					return true, nil
				}
				if reflect.DeepEqual(oldBase.Labels, newBase.Labels) && reflect.DeepEqual(oldBase.Spec, newBase.Spec) {
					// Decide whether discovery has reported labels or spec change.
					klog.V(4).Infof("no updates on the labels and spec of Base %s, skipping syncing", klog.KObj(oldBase))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	err := baseInformer.Informer().AddIndexers(cache.Indexers{known.IndexKeyForBaseUID: utils.BaseUidIndexFunc})
	if err != nil {
		return nil, err
	}
	err = baseInformer.Informer().AddIndexers(cache.Indexers{known.IndexKeyForSubscriptionUID: utils.BaseSubUidIndexFunc})
	if err != nil {
		return nil, err
	}

	// Manage the addition/update of Base
	_, err = baseInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
	if err != nil {
		return nil, err
	}

	_, err = descInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			desc, ok := obj.(*appsapi.Description)
			if !ok {
				tombstone, ok2 := obj.(cache.DeletedFinalStateUnknown)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("couldn't get object from tombstone %#v", obj))
					return
				}
				desc, ok2 = tombstone.Obj.(*appsapi.Description)
				if !ok2 {
					utilruntime.HandleError(fmt.Errorf("tombstone contained object that is not a Description %#v", obj))
					return
				}
			}

			controllerRef := &metav1.OwnerReference{
				Kind: desc.Labels[known.ConfigKindLabel],
				Name: desc.Labels[known.ConfigNameLabel],
				UID:  types.UID(desc.Labels[known.ConfigUIDLabel]),
			}
			base := c.resolveControllerRef(desc.Labels[known.ConfigNamespaceLabel], controllerRef)
			if base == nil {
				return
			}
			klog.V(4).Infof("deleting Description %q", klog.KObj(desc))
			yachtController.Enqueue(base)
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
func (c *Controller) resolveControllerRef(namespace string, controllerRef *metav1.OwnerReference) *appsapi.Base {
	// We can't look up by UID, so look up by Name and then verify UID.
	// Don't even try to look up by Name if it's the wrong Kind.
	if controllerRef.Kind != controllerKind.Kind {
		return nil
	}

	base, err := c.baseLister.Bases(namespace).Get(controllerRef.Name)
	if err != nil {
		return nil
	}
	if base.UID != controllerRef.UID {
		// The controller we found with this Name is not the same one that the
		// ControllerRef points to.
		return nil
	}
	return base
}

// handle compares the actual state with the desired, and attempts to
// converge the two. It then updates the Status block of the Base resource
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

	klog.V(4).Infof("start processing Base %q", key)
	// Get the Base resource with this name
	cachedBase, err := c.baseLister.Bases(ns).Get(name)
	// The Base resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("Base %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// add finalizer
	base := cachedBase.DeepCopy()
	if !utils.ContainsString(base.Finalizers, known.AppFinalizer) && base.DeletionTimestamp == nil {
		base.Finalizers = append(base.Finalizers, known.AppFinalizer)
		base, err = c.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Update(context.TODO(),
			base, metav1.UpdateOptions{})
		if err != nil {
			msg := fmt.Sprintf("failed to inject finalizer %s to Base %s: %v", known.AppFinalizer, klog.KObj(base), err)
			klog.WarningDepth(4, msg)
			c.recorder.Event(base, corev1.EventTypeWarning, "FailedInjectingFinalizer", msg)
			return nil, err
		}
		msg := fmt.Sprintf("successfully inject finalizer %s to Base %s", known.AppFinalizer, klog.KObj(base))
		klog.V(4).Info(msg)
		c.recorder.Event(base, corev1.EventTypeNormal, "FinalizerInjected", msg)
	}

	// label Base self uid
	if val, ok := base.Labels[string(base.UID)]; !ok || val != controllerKind.Kind {
		base, err = c.patchBaseLabels(base, map[string]*string{
			string(base.UID): utilpointer.StringPtr(controllerKind.Kind),
		})
		if err != nil {
			klog.ErrorDepth(5, fmt.Sprintf("failed to patch Base %s labels: %v", klog.KObj(base), err))
			return nil, err
		}
	}

	base.Kind = controllerKind.Kind
	base.APIVersion = controllerKind.GroupVersion().String()
	err = c.syncHandlerFunc(base)
	if err != nil {
		c.recorder.Event(base, corev1.EventTypeWarning, "FailedSynced", err.Error())
	} else {
		c.recorder.Event(base, corev1.EventTypeNormal, "Synced", "Base synced successfully")
		klog.Infof("successfully synced Base %q", key)
	}
	return nil, err
}

func (c *Controller) patchBaseLabels(base *appsapi.Base, labels map[string]*string) (*appsapi.Base, error) {
	if base.DeletionTimestamp != nil || len(labels) == 0 {
		return base, nil
	}

	klog.V(5).Infof("patching Base %s labels", klog.KObj(base))
	option := utils.MetaOption{MetaData: utils.MetaData{Labels: labels}}
	patchData, err := utils.Marshal(option)
	if err != nil {
		return nil, err
	}

	return c.clusternetClient.AppsV1alpha1().Bases(base.Namespace).Patch(context.TODO(),
		base.Name,
		types.MergePatchType,
		patchData,
		metav1.PatchOptions{})
}

func (c *Controller) FindBaseByUID(uid string) (*appsapi.Base, error) {
	objs, err := c.baseIndexer.ByIndex(known.IndexKeyForBaseUID, uid)
	if err != nil || len(objs) == 0 {
		return nil, fmt.Errorf("find base by uid %s failed: %v %d", uid, err, len(objs))
	}

	return objs[0].(*appsapi.Base), nil
}

func (c *Controller) FindBaseBySubUID(subUid string) ([]*appsapi.Base, error) {
	objs, err := c.baseIndexer.ByIndex(known.IndexKeyForSubscriptionUID, subUid)
	if err != nil {
		return nil, fmt.Errorf("find base by subUid %s failed: %v", subUid, err)
	}

	var bases []*appsapi.Base
	for _, base := range objs {
		bases = append(bases, base.(*appsapi.Base))
	}
	return bases, nil
}
