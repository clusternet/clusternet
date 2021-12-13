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

type resourceInformer struct {
	controller cache.Controller
	store      cache.Store
	stopChan   chan struct{}
}

func (ri *resourceInformer) Start() {
	go ri.controller.Run(ri.stopChan)
}

func (ri *resourceInformer) HasSynced() bool {
	return ri.controller.HasSynced()
}

// NewResourceInformer returns a filtered informer limited to resources managed by Clusternet
//// as indicated by labeling.
func NewResourceInformer(client ResourceClient, apiResource *metav1.APIResource,
	handler cache.ResourceEventHandlerFuncs) *resourceInformer {
	store, controller := newResourceInformer(client, apiResource, handler)
	return &resourceInformer{
		controller: controller,
		store:      store,
		stopChan:   make(chan struct{}),
	}
}

func newResourceInformer(client ResourceClient, apiResource *metav1.APIResource,
	handler cache.ResourceEventHandlerFuncs) (cache.Store, cache.Controller) {
	labelSelector := labels.Set(map[string]string{
		known.ObjectCreatedByLabel: known.ClusternetHubName,
	}).AsSelector().String()

	obj := &unstructured.Unstructured{}

	if apiResource != nil {
		gvk := schema.GroupVersionKind{Group: apiResource.Group, Version: apiResource.Version, Kind: apiResource.Kind}
		obj.SetGroupVersionKind(gvk)
	}
	return cache.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (pkgruntime.Object, error) {
				options.LabelSelector = labelSelector
				return client.Resources("").List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				options.LabelSelector = labelSelector
				return client.Resources("").Watch(context.Background(), options)
			},
		},
		obj, // use an unstructured type with apiVersion / kind populated for informer logging purposes
		known.NoResyncPeriod,
		handler,
	)
}
