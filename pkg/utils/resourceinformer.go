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
//This file was copied from sigs.k8s.io/kubefed/pkg/controller/util/resourceinformer.go
package utils

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	pkgruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	"github.com/clusternet/clusternet/pkg/known"
)

type informer struct {
	controller cache.Controller
	store      cache.Store
	stopChan   chan struct{}
}

// RawresourceInformer provides access to

type ResourceInformer interface {

	// Starts all the processes.
	Start()

	// Stops all the processes inside the informer.
	HasSynced() bool
}

func (infr informer) Start() {
	go infr.controller.Run(infr.stopChan)
}

func (infr informer) HasSynced() bool {
	return infr.controller.HasSynced()
}

// Builds a NewResourceInformer for the given configuration.
func NewResourceInformer(client ResourceClient, targetNamespace string,
	apiResource *metav1.APIResource, handler cache.ResourceEventHandlerFuncs) informer {

	//resourceClient, _:= NewResourceClient(client, apiResource)
	store, controller := NewManagedResourceInformer(client, targetNamespace, apiResource, handler)
	targetInformer := informer{
		controller: controller,
		store:      store,
		stopChan:   make(chan struct{}),
	}

	return targetInformer
}

// NewManagedResourceInformer returns an informer limited to resources
// managed by KubeFed as indicated by labeling.
func NewManagedResourceInformer(client ResourceClient, namespace string, apiResource *metav1.APIResource, handler cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller) {
	labelSelector := labels.Set(map[string]string{known.ObjectCreatedByLabel: known.ClusternetHubName}).AsSelector().String()
	return newResourceInformer(client, namespace, apiResource, handler, labelSelector)
}

func newResourceInformer(client ResourceClient, namespace string, apiResource *metav1.APIResource, handler cache.ResourceEventHandlerFuncs, labelSelector string) (cache.Store, cache.Controller) {
	obj := &unstructured.Unstructured{}

	if apiResource != nil {
		gvk := schema.GroupVersionKind{Group: apiResource.Group, Version: apiResource.Version, Kind: apiResource.Kind}
		obj.SetGroupVersionKind(gvk)
	}
	return cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (pkgruntime.Object, error) {
				options.LabelSelector = labelSelector
				return client.Resources(namespace).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelector
				return client.Resources(namespace).Watch(context.Background(), options)
			},
		},
		obj, // use an unstructured type with apiVersion / kind populated for informer logging purposes
		known.NoResyncPeriod,
		handler,
	)
}
