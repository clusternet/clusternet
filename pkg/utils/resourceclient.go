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

// This file was copied from sigs.k8s.io/kubefed/pkg/controller/util/resourceclient.go and modified.

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

type ResourceClient interface {
	Resources(namespace string) dynamic.ResourceInterface
	Kind() string
}

type resourceClient struct {
	client      dynamic.Interface
	apiResource schema.GroupVersionResource
	namespaced  bool
	kind        string
}

func NewResourceClient(client dynamic.Interface, apiResource *metav1.APIResource) ResourceClient {
	return &resourceClient{
		client: client,
		apiResource: schema.GroupVersionResource{
			Group:    apiResource.Group,
			Version:  apiResource.Version,
			Resource: apiResource.Name,
		},
		namespaced: apiResource.Namespaced,
		kind:       apiResource.Kind,
	}
}

func (c *resourceClient) Resources(namespace string) dynamic.ResourceInterface {
	// TODO(marun) Consider returning Interface instead of
	// ResourceInterface to allow callers to decide if they want to
	// invoke Namespace().  Either that, or replace the use of
	// ResourceClient with the controller-runtime generic client.
	if c.namespaced {
		return c.client.Resource(c.apiResource).Namespace(namespace)
	}
	return c.client.Resource(c.apiResource)
}

func (c *resourceClient) Kind() string {
	return c.kind
}
