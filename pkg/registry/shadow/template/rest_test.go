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

package template

import (
	"testing"
)

func TestRESTGenerateNameForManifest(t *testing.T) {
	tests := []struct {
		testCaseName string
		resourceName string
		namespace    string
		namespaced   bool
		name         string
		want         string
	}{
		{
			testCaseName: "namespace-scoped resources foos (standard)",
			resourceName: "foos",
			namespace:    "kube-system",
			namespaced:   true,
			name:         "abc",
			want:         "foos.kube-system.abc",
		},
		{
			testCaseName: "namespace-scoped resources foos (name with '.' & '-')",
			resourceName: "foos",
			namespace:    "kube-system",
			namespaced:   true,
			name:         "abc.def-bar",
			want:         "foos.kube-system.abc.def-bar",
		},

		{
			testCaseName: "cluster-scoped resources bars (standard)",
			resourceName: "bars",
			namespace:    "kube-system",
			namespaced:   false,
			name:         "abc",
			want:         "bars.abc",
		},
		{
			testCaseName: "cluster-scoped resources bars (name with '.' & '-')",
			resourceName: "bars",
			namespace:    "kube-system",
			namespaced:   false,
			name:         "abc.def-bar",
			want:         "bars.abc.def-bar",
		},
	}
	for _, tt := range tests {
		t.Run(tt.testCaseName, func(t *testing.T) {
			r := &REST{
				name:       tt.resourceName,
				namespaced: tt.namespaced,
			}
			if got := r.generateNameForManifest(tt.namespace, tt.name); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
