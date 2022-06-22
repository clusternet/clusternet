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
	"reflect"
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
		{
			name: "url does not contain proxy prefix",
			req: &http.Request{
				URL: &url.URL{
					Path:     "/connect",
					RawQuery: "abc=def",
				},
			},
			clusterID: "",
			authed:    false,
			wantErr:   true,
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

// modified on k8s.io/apiserver/pkg/authentication/request/headerrequest/requestheader_test.go
func TestGetExtraFromHeaders(t *testing.T) {
	tests := []struct {
		name           string
		header         http.Header
		headerPrefixes []string
		wantedExtra    map[string][]string
	}{
		{
			name:        "empty",
			wantedExtra: map[string][]string{},
		},
		{
			name:           "extra not match 1",
			header:         http.Header{"X-Remote-User": {"Bob"}},
			headerPrefixes: []string{"X-Remote-Extra-"},
			wantedExtra:    map[string][]string{},
		},
		{
			name: "exact not match 2",
			header: http.Header{
				"X-Remote-Extra":          {"", "First header, second value"},
				"A-Second-X-Remote-Extra": {"Second header, first value", "Second header, second value"},
				"Another-X-Remote-Extra":  {"Third header, first value"}},
			headerPrefixes: []string{"X-Remote-Extra-"},
			wantedExtra:    map[string][]string{},
		},
		{
			name: "extra prefix matches",
			header: http.Header{
				"X-Remote-Extra-User": {"Bob", "Tom"},
			},
			headerPrefixes: []string{"X-Remote-Extra-"},
			wantedExtra: map[string][]string{
				"user": {"Bob", "Tom"},
			},
		},
		{
			name: "extra prefix matches case-insensitive",
			header: http.Header{
				"X-Remote-User":         {"Bob"},
				"X-Remote-Group-1":      {"one-a", "one-b"},
				"X-Remote-Group-2":      {"two-a", "two-b"},
				"X-Remote-extra-1-key1": {"alfa", "bravo"},
				"X-Remote-Extra-1-Key2": {"charlie", "delta"},
				"X-Remote-Extra-1-":     {"india", "juliet"},
				"X-Remote-extra-2-":     {"kilo", "lima"},
				"X-Remote-extra-2-Key1": {"echo", "foxtrot"},
				"X-Remote-Extra-2-key2": {"golf", "hotel"},
			},
			headerPrefixes: []string{"X-Remote-Extra-1-", "X-Remote-Extra-2-"},
			wantedExtra: map[string][]string{
				"key1": {"alfa", "bravo", "echo", "foxtrot"},
				"key2": {"charlie", "delta", "golf", "hotel"},
				"":     {"india", "juliet", "kilo", "lima"},
			},
		},
		{
			name: "escaped extra keys",
			header: http.Header{
				"X-Remote-User":                                            {"Bob"},
				"X-Remote-Group":                                           {"one-a", "one-b"},
				"X-Remote-Extra-Alpha":                                     {"alphabetical"},
				"X-Remote-Extra-Alph4num3r1c":                              {"alphanumeric"},
				"X-Remote-Extra-Percent%20encoded":                         {"percent encoded"},
				"X-Remote-Extra-Almost%zzpercent%xxencoded":                {"not quite percent encoded"},
				"X-Remote-Extra-Example.com%2fpercent%2520encoded":         {"url with double percent encoding"},
				"X-Remote-Extra-Example.com%2F%E4%BB%8A%E6%97%A5%E3%81%AF": {"url with unicode"},
				"X-Remote-Extra-Abc123!#$+.-_*\\^`~|'":                     {"header key legal characters"},
			},
			headerPrefixes: []string{"X-Remote-Extra-"},
			wantedExtra: map[string][]string{
				"alpha":                         {"alphabetical"},
				"alph4num3r1c":                  {"alphanumeric"},
				"percent encoded":               {"percent encoded"},
				"almost%zzpercent%xxencoded":    {"not quite percent encoded"},
				"example.com/percent%20encoded": {"url with double percent encoding"},
				"example.com/今日は":               {"url with unicode"},
				"abc123!#$+.-_*\\^`~|'":         {"header key legal characters"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getExtraFromHeaders(tt.header, tt.headerPrefixes); !reflect.DeepEqual(got, tt.wantedExtra) {
				t.Errorf("getExtraFromHeaders() = %v, wantedExtra %v", got, tt.wantedExtra)
			}
		})
	}
}
