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

package clientgo

import (
	"testing"
)

func TestNormalizeLocation(t *testing.T) {
	tests := []struct {
		name          string
		host          string
		requestURL    string
		normalizedURL string
	}{
		{
			name:          "apiv1 - list namespaces",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces",
		},
		{
			name:          "apiv1 - get namespace abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc",
		},
		{
			name:          "apiv1 - watch namespaces",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces?resourceVersion=17825795&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces?resourceVersion=17825795&watch=true",
		},
		{
			name:          "apiv1 - watch namespace abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},

		{
			name:          "apiv1 - list serviceaccounts",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc/serviceaccounts",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/serviceaccounts",
		},
		{
			name:          "apiv1 - get serviceaccounts abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc/serviceaccounts/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/serviceaccounts/abc",
		},
		{
			name:          "apiv1 - watch serviceaccounts",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc/serviceaccounts?resourceVersion=17825795&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/serviceaccounts?resourceVersion=17825795&watch=true",
		},
		{
			name:          "apiv1 - watch serviceaccounts abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/api/v1/namespaces/abc/abc/serviceaccounts/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/abc/serviceaccounts/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},

		{
			name:          "apis - list namespace-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/vbar/namespaces/abc/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/foos",
		},
		{
			name:          "apis - get namespace-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v1bar/namespaces/abc/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/foos/abc",
		},
		{
			name:          "apis - watch namespace-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v1bar1/namespaces/abc/foos?resourceVersion=17825795&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/foos?resourceVersion=17825795&watch=true",
		},
		{
			name:          "apis - watch namespace-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v2/namespaces/abc/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/abc/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},

		{
			name:          "apis - list cluster-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos",
		},
		{
			name:          "apis - get cluster-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v1bar/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - watch cluster-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v1bar1/foos?resourceVersion=17825795&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos?resourceVersion=17825795&watch=true",
		},
		{
			name:          "apis - watch cluster-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far/v2/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},

		{
			name:          "clusternet - list cluster-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far.clusternet.io/vbar/foos",
			normalizedURL: "https://my.k8s.io/apis/far.clusternet.io/vbar/foos",
		},
		{
			name:          "clusternet - get cluster-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far.clusternet.io/v1bar/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/far.clusternet.io/v1bar/foos/abc",
		},
		{
			name:          "clusternet - watch cluster-scoped foos",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far.clusternet.io/v1bar1/foos?resourceVersion=17825795&watch=true",
			normalizedURL: "https://my.k8s.io/apis/far.clusternet.io/v1bar1/foos?resourceVersion=17825795&watch=true",
		},
		{
			name:          "clusternet - watch cluster-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far.clusternet.io/v2/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/far.clusternet.io/v2/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},
		{
			name:          "clusternet - watch namespace-scoped foos abc",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/far.clusternet.io/v1bar/namespaces/boo/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/far.clusternet.io/v1bar/namespaces/boo/foos/abc?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},
		{
			name:          "clusternet - support crd with group samplecontroller.k8s.io",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/samplecontroller.k8s.io/v1alpha1/namespaces/default/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/default/foos",
		},
		{
			name:          "clusternet - support crd with group a.foo",
			host:          "https://my.k8s.io",
			requestURL:    "https://my.k8s.io/apis/a.foo/v1alpha1/namespaces/default/foos?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/namespaces/default/foos?fieldSelector=metadata.name%3Dabc&resourceVersion=0&watch=true",
		},

		{
			name:          "apiv1 - host with trailing slash",
			host:          "https://my.k8s.io/",
			requestURL:    "https://my.k8s.io/api/v1/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos",
		},
		{
			name:          "apis - host with trailing slash",
			host:          "https://my.k8s.io/",
			requestURL:    "https://my.k8s.io/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos",
		},
		{
			name:          "apiv1 - host with trailing slashes",
			host:          "https://my.k8s.io//",
			requestURL:    "https://my.k8s.io/api/v1/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - host with trailing slashes",
			host:          "https://my.k8s.io//",
			requestURL:    "https://my.k8s.io/apis/far/vbar/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apiv1 - host with query params",
			host:          "https://my.k8s.io?abc=def",
			requestURL:    "https://my.k8s.io/api/v1/foos/abc",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - host with query params",
			host:          "https://my.k8s.io?abc=def",
			requestURL:    "https://my.k8s.io/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/apis/shadow/v1alpha1/foos",
		},
		{
			name:          "apiv1 - host with subpath",
			host:          "https://my.k8s.io/abc",
			requestURL:    "https://my.k8s.io/abc/api/v1/foos/abc",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - host with subpath",
			host:          "https://my.k8s.io/abc",
			requestURL:    "https://my.k8s.io/abc/apis/far/vbar/foos/abc",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apiv1 - host with subpath and slashes",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/api/v1/foos/abc",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - host with subpath and slashes",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos",
		},
		{
			name:          "apiv1 - host with subpath and slashes, request url ends with subpath",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/api/v1/foos/abc",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apis - host with subpath and slashes, request url ends with subpath",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/apis/far/vbar/foos/abc",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc",
		},
		{
			name:          "apiv1 - host with subpath and slashes, request url contains subpath",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/api/v1/foos/abc/def",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc/def",
		},
		{
			name:          "apis - host with subpath and slashes, request url contains subpath",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/apis/far/vbar/foos/abc/def",
			normalizedURL: "https://my.k8s.io/abc/apis/shadow/v1alpha1/foos/abc/def",
		},

		{
			name:          "apiv1 proxies by clusternet",
			host:          "https://my.k8s.io/",
			requestURL:    "https://my.k8s.io/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/api/v1/foos",
			normalizedURL: "https://my.k8s.io/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/api/v1/foos",
		},
		{
			name:          "apis proxies by clusternet",
			host:          "https://my.k8s.io/",
			requestURL:    "https://my.k8s.io/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/apis/far/vbar/foos",
		},
		{
			name:          "apis proxies by clusternet - subpath",
			host:          "https://my.k8s.io/abc////////",
			requestURL:    "https://my.k8s.io/abc/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/apis/far/vbar/foos",
			normalizedURL: "https://my.k8s.io/abc/apis/proxies.clusternet.io/v1alpha1/sockets/abc/https/another.k8s.io/apis/far/vbar/foos",
		},
	}

	for _, tt := range tests {
		transport := NewClusternetTransport(tt.host, nil)
		location := urlMustParse(tt.requestURL)
		transport.normalizeLocation(location)

		if location.String() != tt.normalizedURL {
			t.Errorf("%s: incorrect normalization, WANT %q, but GOT %q ", tt.name, tt.normalizedURL, location.String())
		}
	}
}
