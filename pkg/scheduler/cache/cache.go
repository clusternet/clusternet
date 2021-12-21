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

package cache

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusterlisters "github.com/clusternet/clusternet/pkg/generated/listers/clusters/v1beta1"
)

type Cache interface {
	// NumClusters returns the number of clusters in the cache.
	NumClusters() int

	// List returns the list of ManagedCluster(s).
	List(labelSelector *metav1.LabelSelector) ([]*clusterapi.ManagedCluster, error)

	// Get returns the ManagedCluster of the given managed cluster.
	Get(namespacedName string) (*clusterapi.ManagedCluster, error)
}

type schedulerCache struct {
	clusterListers clusterlisters.ManagedClusterLister
}

// NumClusters returns the number of clusters in the cache.
func (s *schedulerCache) NumClusters() int {
	clusters, err := s.List(&metav1.LabelSelector{})
	if err != nil {
		klog.Errorf("failed to list clusters: %v", err)
		return 0
	}
	return len(clusters)
}

// List returns the list of clusters in the cache.
func (s *schedulerCache) List(labelSelector *metav1.LabelSelector) ([]*clusterapi.ManagedCluster, error) {
	selector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nil, err
	}
	return s.clusterListers.List(selector)
}

// Get returns the ManagedCluster of the given cluster.
func (s *schedulerCache) Get(namespacedName string) (*clusterapi.ManagedCluster, error) {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedName)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", namespacedName))
		return nil, err
	}

	return s.clusterListers.ManagedClusters(ns).Get(name)
}

func New(clusterListers clusterlisters.ManagedClusterLister) Cache {
	return &schedulerCache{
		clusterListers: clusterListers,
	}
}
