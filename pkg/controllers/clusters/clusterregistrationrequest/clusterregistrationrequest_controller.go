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

package clusterregistrationrequest

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/dixudx/yacht"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternetClientSet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	crrsInformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	crrsListers "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
)

// controllerKind contains the schema.GroupVersionKind for this controller type.
var controllerKind = clusterapi.SchemeGroupVersion.WithKind("ClusterRegistrationRequest")

type SyncHandlerFunc func(*clusterapi.ClusterRegistrationRequest) error

// Controller is a controller that handles edge cluster registration requests
type Controller struct {
	yachtController *yacht.Controller

	clusternetClient clusternetClientSet.Interface
	crrsLister       crrsListers.ClusterRegistrationRequestLister
	syncHandler      SyncHandlerFunc
}

// NewController creates and initializes a new Controller
func NewController(
	clusternetClient clusternetClientSet.Interface,
	crrsInformer crrsInformers.ClusterRegistrationRequestInformer,
	syncHandler SyncHandlerFunc,
) (*Controller, error) {
	c := &Controller{
		clusternetClient: clusternetClient,
		crrsLister:       crrsInformer.Lister(),
		syncHandler:      syncHandler,
	}
	// create a yacht controller for cluster-registration-requests
	yachtController := yacht.NewController("cluster-registration-requests").
		WithCacheSynced(
			crrsInformer.Informer().HasSynced,
		).
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: Decide whether discovery has reported a spec change.
			if oldObj != nil && newObj != nil {
				oldCrr := oldObj.(*clusterapi.ClusterRegistrationRequest)
				newCrr := newObj.(*clusterapi.ClusterRegistrationRequest)
				if newCrr.DeletionTimestamp != nil {
					return true, nil
				}
				if reflect.DeepEqual(oldCrr.Spec, newCrr.Spec) {
					klog.V(4).Infof("no updates on the spec of ClusterRegistrationRequest %q, skipping syncing", oldCrr.Name)
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// Manage the addition/update of ClusterRegistrationRequest
	_, err := crrsInformer.Informer().AddEventHandler(yachtController.DefaultResourceEventHandlerFuncs())
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
// converge the two. It then updates the Status block of the ClusterRegistrationRequest resource
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

	klog.V(4).Infof("start processing ClusterRegistrationRequest %q", key)
	// Get the ClusterRegistrationRequest resource with this name
	crr, err := c.crrsLister.Get(name)
	// The ClusterRegistrationRequest resource may no longer exist, in which case we stop processing.
	if errors.IsNotFound(err) {
		klog.V(2).Infof("ClusterRegistrationRequest %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if len(crr.Kind) == 0 {
		crr.Kind = controllerKind.Kind
	}
	if len(crr.APIVersion) == 0 {
		crr.APIVersion = controllerKind.Version
	}

	err = c.syncHandler(crr.DeepCopy())
	if err == nil {
		klog.Infof("successfully synced ClusterRegistrationRequest %q", key)
	}
	return nil, err
}

func (c *Controller) UpdateCRRStatus(crr *clusterapi.ClusterRegistrationRequest, status *clusterapi.ClusterRegistrationRequestStatus) error {
	// NEVER modify objects from the store. It's a read-only, local cache.
	// You can use DeepCopy() to make a deep copy of original object and modify this copy
	// Or create a copy manually for better performance

	klog.V(5).Infof("try to update ClusterRegistrationRequest %q status", crr.Name)
	if status.Result != nil && *status.Result != clusterapi.RequestApproved {
		if status.ErrorMessage == "" {
			return fmt.Errorf("for non approved requests, must set an error message")
		}

		// remove sensitive data if the request is not approved
		status.DedicatedToken = nil
		status.ManagedClusterName = ""
		status.DedicatedNamespace = ""
		status.CACertificate = nil
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		crr.Status = *status
		_, err := c.clusternetClient.ClustersV1beta1().ClusterRegistrationRequests().UpdateStatus(context.TODO(), crr, metav1.UpdateOptions{})
		if err == nil {
			klog.V(4).Infof("successfully update status of ClusterRegistrationRequest %q to %q", crr.Name,
				*status.Result)
			return nil
		}

		updated, err2 := c.crrsLister.Get(crr.Name)
		if err2 == nil {
			// make a copy, so we don't mutate the shared cache
			crr = updated.DeepCopy()
			return nil
		}
		utilruntime.HandleError(fmt.Errorf("error getting updated ClusterRegistrationRequest %q from lister: %v",
			crr.Name, err2))
		return err2
	})
}
