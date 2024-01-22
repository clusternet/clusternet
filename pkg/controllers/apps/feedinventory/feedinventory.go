/*
Copyright 2022 The Clusternet Authors.

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

package feedinventory

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
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

var subKind = appsapi.SchemeGroupVersion.WithKind("Subscription")

// SyncHandlerFunc is the function to sync a Subscription object
type SyncHandlerFunc func(subscription *appsapi.Subscription) error

// Controller is a controller that maintains FeedInventory objects
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient      clusternetclientset.Interface
	subLister             applisters.SubscriptionLister
	finvLister            applisters.FeedInventoryLister
	manifestLister        applisters.ManifestLister
	recorder              record.EventRecorder
	registry              Registry
	customSyncHandlerFunc SyncHandlerFunc

	// namespace where Manifests are created
	reservedNamespace string
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	subsInformer appinformers.SubscriptionInformer,
	finvInformer appinformers.FeedInventoryInformer,
	manifestInformer appinformers.ManifestInformer,
	recorder record.EventRecorder,
	registry Registry,
	reservedNamespace string,
	customSyncHandlerFunc SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:      clusternetClient,
		subLister:             subsInformer.Lister(),
		finvLister:            finvInformer.Lister(),
		manifestLister:        manifestInformer.Lister(),
		recorder:              recorder,
		registry:              registry,
		reservedNamespace:     reservedNamespace,
		customSyncHandlerFunc: customSyncHandlerFunc,
	}
	// create a yacht controller for feedInventory
	yachtController := yacht.NewController("feedInventory").
		WithCacheSynced(
			subsInformer.Informer().HasSynced,
			finvInformer.Informer().HasSynced,
			manifestInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// FeedInventory has the same name with Subscription

			// UPDATE
			if oldObj != nil && newObj != nil {
				oldSub := oldObj.(*appsapi.Subscription)
				newSub := newObj.(*appsapi.Subscription)

				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldSub.Spec, newSub.Spec) {
					klog.V(4).Infof("[FeedInventory] no updates on the spec of Subscription %s, skipping syncing", klog.KObj(newSub))
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

	_, err = finvInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj interface{}) {
			// FeedInventory has the same name with Subscription
			yachtController.Enqueue(obj)
		},
	})
	if err != nil {
		return nil, err
	}

	// When a resource gets scaled, such as Deployment,
	// re-enqueue all referring Subscription objects
	_, err = manifestInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			manifest := obj.(*appsapi.Manifest)
			klog.V(4).Infof("[FeedInventory] adding Manifest %q", klog.KObj(manifest))
			c.enqueueManifest(manifest)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldManifest := oldObj.(*appsapi.Manifest)
			newManifest := newObj.(*appsapi.Manifest)
			// Decide whether discovery has reported a spec change.
			if reflect.DeepEqual(oldManifest.Template, newManifest.Template) {
				klog.V(4).Infof("[FeedInventory] no updates on Manifest template %s, skipping syncing", klog.KObj(oldManifest))
				return
			}
			klog.V(4).Infof("[FeedInventory] updating Manifest %q", klog.KObj(oldManifest))
			c.enqueueManifest(newManifest)
		},
	})
	if err != nil {
		return nil, err
	}

	// TODO: helm charts ?

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

	klog.V(4).Infof("[FeedInventory] start processing Subscription %q", key)
	// Get the Subscription resource with this name
	cachedSub, err := c.subLister.Subscriptions(ns).Get(name)
	// The Subscription resource may no longer exist, in which case we stop processing.
	if apierrors.IsNotFound(err) {
		klog.V(2).Infof("Subscription %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	sub := cachedSub.DeepCopy()
	if sub.DeletionTimestamp != nil {
		return nil, nil
	}

	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return nil, nil
	}

	sub.Kind = subKind.Kind
	sub.APIVersion = subKind.GroupVersion().String()
	if c.customSyncHandlerFunc != nil {
		return nil, c.customSyncHandlerFunc(sub)
	}
	return nil, c.handleSubscription(sub)
}

// enqueueManifest takes a Manifest resource and converts it into a namespace/name
// string which is then put onto the work queue.
func (c *Controller) enqueueManifest(obj interface{}) {
	manifest := obj.(*appsapi.Manifest)
	if manifest.DeletionTimestamp != nil {
		return
	}

	subUIDs := []string{}
	for k, v := range manifest.GetLabels() {
		if v == subKind.Kind {
			subUIDs = append(subUIDs, k)
		}
	}

	allSubs := []*appsapi.Subscription{}
	for _, subUID := range subUIDs {
		subs, err := c.subLister.List(labels.SelectorFromSet(labels.Set{
			subUID: subKind.Kind,
		}))
		if err != nil {
			utilruntime.HandleError(err)
			return
		}
		allSubs = append(allSubs, subs...)
	}

	for _, sub := range allSubs {
		c.yachtController.Enqueue(sub)
	}
}

func (c *Controller) handleSubscription(sub *appsapi.Subscription) error {
	finv := &appsapi.FeedInventory{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sub.Name,
			Namespace: sub.Namespace,
			Labels: map[string]string{
				known.ObjectCreatedByLabel: known.ClusternetCtrlMgrName,
				// add subscription info
				known.ConfigSubscriptionNameLabel:      sub.Name,
				known.ConfigSubscriptionNamespaceLabel: sub.Namespace,
				known.ConfigSubscriptionUIDLabel:       string(sub.UID),
			},
		},
		Spec: appsapi.FeedInventorySpec{
			Feeds: make([]appsapi.FeedOrder, len(sub.Spec.Feeds)),
		},
	}
	finv.SetOwnerReferences([]metav1.OwnerReference{*metav1.NewControllerRef(sub, subKind)})

	wg := sync.WaitGroup{}
	wg.Add(len(sub.Spec.Feeds))
	errCh := make(chan error, len(sub.Spec.Feeds))
	for idx, feed := range sub.Spec.Feeds {
		go func(idx int, feed appsapi.Feed) {
			defer wg.Done()

			manifests, err2 := utils.ListManifestsBySelector(c.reservedNamespace, c.manifestLister, feed)
			if err2 != nil {
				errCh <- err2
				return
			}
			if manifests == nil {
				errCh <- apierrors.NewNotFound(schema.GroupResource{Resource: feed.Kind}, feed.Name)
				return
			}

			gvk, err2 := getGroupVersionKind(manifests[0].Template.Raw)
			if err2 != nil {
				errCh <- err2
				return
			}

			var desiredReplicas *int32
			var replicaRequirements appsapi.ReplicaRequirements
			var replicaJsonPath string
			plugin, ok := c.registry[gvk]
			if ok {
				klog.V(7).Infof("using plugin %q to parse %q", plugin.Name(), plugin.Kind())
				desiredReplicas, replicaRequirements, replicaJsonPath, err2 = plugin.Parser(manifests[0].Template.Raw)
				if err2 != nil {
					errCh <- err2
					return
				}
			}

			finv.Spec.Feeds[idx] = appsapi.FeedOrder{
				Feed:                feed,
				DesiredReplicas:     desiredReplicas,
				ReplicaRequirements: replicaRequirements,
				ReplicaJsonPath:     replicaJsonPath,
			}
		}(idx, feed)
	}

	wg.Wait()

	// collect errors
	close(errCh)
	var allErrs []error
	for err3 := range errCh {
		allErrs = append(allErrs, err3)
	}

	var err error
	if len(allErrs) != 0 {
		err = utilerrors.NewAggregate(allErrs)
		c.recorder.Event(sub, corev1.EventTypeWarning, "UnParseableFeeds", err.Error())
		return err
	}

	_, err = c.clusternetClient.AppsV1alpha1().FeedInventories(finv.Namespace).Create(context.TODO(), finv, metav1.CreateOptions{})
	if err != nil && apierrors.IsAlreadyExists(err) {
		curFinv, err2 := c.finvLister.FeedInventories(finv.Namespace).Get(finv.Name)
		if err2 != nil {
			return err2
		}
		finv.SetResourceVersion(curFinv.GetResourceVersion())
		_, err = c.clusternetClient.AppsV1alpha1().FeedInventories(finv.Namespace).Update(context.TODO(), finv, metav1.UpdateOptions{})
	}

	if err != nil {
		c.recorder.Event(sub, corev1.EventTypeWarning, "FeedInventoryNotSynced", err.Error())
	} else {
		c.recorder.Event(sub, corev1.EventTypeNormal, "FeedInventorySynced", "FeedInventory synced successfully")
		klog.Infof("[FeedInventory] successfully synced Subscription %q", klog.KObj(sub).String())
	}
	return err
}

func getGroupVersionKind(rawData []byte) (schema.GroupVersionKind, error) {
	object := &unstructured.Unstructured{}
	if err := utils.Unmarshal(rawData, object); err != nil {
		return schema.GroupVersionKind{}, err
	}

	groupVersion, err := schema.ParseGroupVersion(object.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionKind{}, err
	}
	return groupVersion.WithKind(object.GetKind()), nil
}
