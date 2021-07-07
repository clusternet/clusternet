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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:conversion-gen:explicit-from=net/url.Values
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Socket is the query options to a WebSocket proxy call.
type Socket struct {
	metav1.TypeMeta `json:",inline"`

	// Path is the part of URLs that include service endpoints, suffixes,
	// and parameters to use for the current proxy request to child cluster.
	// For example, the whole request URL is
	// http://localhost:8001/apis/proxies.clusternet.io/v1alpha1/sockets/cae6feb5-a23f-4354-8ee7-66527aa03f54/proxy/https/demo.com:6443.
	// Path is /https/demo.com:6443.
	//
	// +optional
	// Path is the URL path to use for the current proxy request
	Path string `json:"path,omitempty"`
}
