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

package exchanger

import (
	"net/http"
	"net/url"
	"testing"
)

func TestAuthorizer(t *testing.T) {
	tests := []struct {
		name      string
		req       *http.Request
		clusterID string
		authed    bool
		wantErr   bool
	}{
		{
			name: "normal url",
			req: &http.Request{
				URL: &url.URL{
					Path: "/apis/proxies.clusternet.io/v1alpha1/sockets/9bb775e3-b177-4e77-8685-1aadcb03b0a8",
				},
			},
			clusterID: "9bb775e3-b177-4e77-8685-1aadcb03b0a8",
			authed:    true,
		},
		{
			name: "normal url with single trailing slash",
			req: &http.Request{
				URL: &url.URL{
					Path: "/apis/proxies.clusternet.io/v1alpha1/sockets/9bb775e3-b177-4e77-8685-1aadcb03b0a8/",
				},
			},
			clusterID: "9bb775e3-b177-4e77-8685-1aadcb03b0a8",
			authed:    true,
		},
		{
			name: "normal url with multiple trailing slashes",
			req: &http.Request{
				URL: &url.URL{
					Path: "/apis/proxies.clusternet.io/v1alpha1/sockets/9bb775e3-b177-4e77-8685-1aadcb03b0a8////",
				},
			},
			clusterID: "9bb775e3-b177-4e77-8685-1aadcb03b0a8",
			authed:    true,
		},
		{
			name: "normal url with multiple trailing slashes and query params",
			req: &http.Request{
				URL: &url.URL{
					Path:     "/apis/proxies.clusternet.io/v1alpha1/sockets/9bb775e3-b177-4e77-8685-1aadcb03b0a8///",
					RawQuery: "abc=def",
				},
			},
			clusterID: "9bb775e3-b177-4e77-8685-1aadcb03b0a8",
			authed:    true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := authorizer(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("authorizer() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.clusterID {
				t.Errorf("authorizer() got = %v, clusterID %v", got, tt.clusterID)
			}
			if got1 != tt.authed {
				t.Errorf("authorizer() got1 = %v, clusterID %v", got1, tt.authed)
			}
		})
	}
}
