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

// This file was copied from k8s.io/kubernetes/pkg/controlplane/controller/apiserverleasegc/gc_controller.go and modified

package leasegc

import (
	"context"
	"fmt"
	"time"

	v1 "k8s.io/api/coordination/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	informers "k8s.io/client-go/informers/coordination/v1"
	"k8s.io/client-go/kubernetes"
	listers "k8s.io/client-go/listers/coordination/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// LeaseFilter is a callback handler that helps filter out uninterested objects.
// It returns true if the lease should pass the filter, else false.
type LeaseFilter func(lease *v1.Lease) bool

// Controller deletes expired clusternet leases.
type Controller struct {
	kubeclientset kubernetes.Interface

	leaseLister   listers.LeaseLister
	leaseInformer cache.SharedIndexInformer
	leasesSynced  cache.InformerSynced

	leaseNamespace string

	gcCheckPeriod time.Duration

	leaseFilter LeaseFilter
}

// NewLeaseGC creates a new Controller.
func NewLeaseGC(clientset kubernetes.Interface, gcCheckPeriod time.Duration, leaseNamespace string, leaseFilter LeaseFilter) *Controller {
	// we construct our own informer because we need such a small subset of the information available.
	// Just one namespace with label selection.
	leaseInformer := informers.NewFilteredLeaseInformer(
		clientset,
		leaseNamespace,
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		nil,
	)
	return &Controller{
		kubeclientset:  clientset,
		leaseLister:    listers.NewLeaseLister(leaseInformer.GetIndexer()),
		leaseInformer:  leaseInformer,
		leasesSynced:   leaseInformer.HasSynced,
		leaseNamespace: leaseNamespace,
		gcCheckPeriod:  gcCheckPeriod,
		leaseFilter:    leaseFilter,
	}
}

// Run starts one worker.
func (c *Controller) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer klog.Infof("Shutting down clusternet lease garbage collector")

	klog.Infof("Starting clusternet lease garbage collector")

	// we have a personal informer that is narrowly scoped, start it.
	go c.leaseInformer.Run(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.leasesSynced) {
		utilruntime.HandleError(fmt.Errorf("timed out waiting for caches to sync"))
		return
	}

	go wait.Until(c.gc, c.gcCheckPeriod, stopCh)

	<-stopCh
}

func (c *Controller) gc() {
	leases, err := c.leaseLister.Leases(c.leaseNamespace).List(labels.Everything())
	if err != nil {
		klog.ErrorS(err, "Error while listing clusternet leases")
		return
	}
	for _, lease := range leases {
		if c.leaseFilter != nil && !c.leaseFilter(lease) {
			continue
		}

		// evaluate lease from cache
		if !isLeaseExpired(lease) {
			continue
		}
		// double check latest lease before deleting
		lease, err = c.kubeclientset.CoordinationV1().Leases(c.leaseNamespace).Get(context.TODO(), lease.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			klog.ErrorS(err, "Error getting lease")
			continue
		}
		if errors.IsNotFound(err) || lease == nil {
			// In an HA cluster, this can happen if the lease was deleted
			// by the same GC controller in another clusternet, which is legit.
			// We don't expect other components to delete the lease.
			klog.V(4).InfoS("Cannot find clusternet lease", "err", err)
			continue
		}
		// evaluate lease from clusternet
		if !isLeaseExpired(lease) {
			continue
		}
		if err = c.kubeclientset.CoordinationV1().Leases(c.leaseNamespace).Delete(
			context.TODO(), lease.Name, metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				// In an HA cluster, this can happen if the lease was deleted
				// by the same GC controller in another clusternet-hub, which is legit.
				// We don't expect other components to delete the lease.
				klog.V(4).InfoS("clusternet lease is gone already", "err", err)
			} else {
				klog.ErrorS(err, "Error deleting lease")
			}
		}
	}
}

func isLeaseExpired(lease *v1.Lease) bool {
	currentTime := time.Now()
	// Leases created by the clusternet-hub peer lease controller should have non-nil renew time
	// and lease duration set. Leases without these fields set are invalid and should
	// be GC'ed.
	return lease.Spec.RenewTime == nil ||
		lease.Spec.LeaseDurationSeconds == nil ||
		lease.Spec.RenewTime.Add(time.Duration(*lease.Spec.LeaseDurationSeconds)*time.Second).Before(currentTime)
}
