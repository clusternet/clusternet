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
	"testing"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func TestGetChildAPIServerProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		mcls    *clusterapi.ManagedCluster
		want    string
		wantErr bool
	}{
		{
			name: "no trailing slash in apiserver url",
			mcls: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					ClusterID: "a-valid-uid",
				},
			},
			want: "https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/a-valid-uid/proxy/direct",
		},
		{
			name: "one trailing slash in apiserver url",
			mcls: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					ClusterID: "a-valid-uid",
				},
			},
			want: "https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/a-valid-uid/proxy/direct",
		},
		{
			name: "multiple trailing slashes in apiserver url",
			mcls: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					ClusterID: "a-valid-uid",
				},
			},
			want: "https://10.0.0.10:6443/apis/proxies.clusternet.io/v1alpha1/sockets/a-valid-uid/proxy/direct",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getChildAPIServerProxyURL("https://10.0.0.10:6443/", tt.mcls)
			if (err != nil) != tt.wantErr {
				t.Errorf("getChildAPIServerProxyURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getChildAPIServerProxyURL() got = %v, want %v", got, tt.want)
			}
		})
	}
}
