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

package localizer

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func TestApplyOverrides(t *testing.T) {
	nameSpaceYaml := `
metadata:
    namespace: test2
`

	removeLabelYaml := `
metadata:
  labels:
    key: null
`

	helmDataYaml := `
address:
  planet: Earth
  street: 234 Spouter Inn Ct.
hole: black
`

	tests := []struct {
		name      string
		original  []byte
		overrides []appsapi.OverrideConfig
		want      []byte
	}{
		{
			name: "Helm",
			original: []byte(`{
				"kind": "Guess",
				"address": {
					"city": "Nantucket",
					"street": "123 Spouter Inn Ct."
				},
				"boat": "pequod",
				"details": {
					"friends": ["Tashtego"]
				},
				"name": "Ishmael"
			}`),
			overrides: []appsapi.OverrideConfig{
				{
					Name:  "empty override",
					Type:  appsapi.HelmType,
					Value: ``,
				},
				{
					Name:  "add/update value - json format",
					Type:  appsapi.HelmType,
					Value: `{"address":{"country":"US","state":"MA"},"boat":"fighter"}`,
				},
				{
					Name:  "add/update value - yaml format",
					Type:  appsapi.HelmType,
					Value: helmDataYaml,
				},
			},
			want: []byte(`{
				"kind": "Guess",
				"address": {
					"city": "Nantucket",
					"country": "US",
					"planet": "Earth",
					"state": "MA",
					"street": "234 Spouter Inn Ct."
                },
				"boat": "fighter",
				"details": {
					"friends": ["Tashtego"]
				},
				"hole": "black",
				"name": "Ishmael"
				}`),
		},
		{
			name: "JSONPatch/MergePatch/StrategicMergePatch",
			original: []byte(`{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
					"name": "pod",
					"labels": {"app": "nginx"}
				},
				"spec": {
					"containers": [{
						"name":  "nginx",
						"image": "nginx:latest"
					}]
				}
			}`),
			overrides: []appsapi.OverrideConfig{
				{
					Name:  "add namespace - json format",
					Type:  appsapi.MergePatchType,
					Value: `{"metadata":{"namespace":"test"}}`,
				},
				{
					Name:  "add namespace - yaml format",
					Type:  appsapi.MergePatchType,
					Value: nameSpaceYaml,
				},
				{
					Name:  "replace container image - 1",
					Type:  appsapi.JSONPatchType,
					Value: `[{"op": "replace", "path": "/spec/containers/0/image", "value":"nginx:1.21.1"}]`,
				},
				{
					Name:  "add and update labels - json format",
					Type:  appsapi.MergePatchType,
					Value: `{"metadata":{"labels":{"foo":"bar","xyz":"def","key":"value"}}}`,
				},
				{
					Name:  "remove labels - json format",
					Type:  appsapi.MergePatchType,
					Value: `{"metadata":{"labels":{"xyz":null}}}`,
				},
				{
					Name:  "remove labels - yaml format",
					Type:  appsapi.MergePatchType,
					Value: removeLabelYaml,
				},
				{
					Name:  "replace container image - 2",
					Type:  appsapi.JSONPatchType,
					Value: `[{"op":"replace","path":"/spec/containers/0/image","value":"nginx:1.20.1"}]`,
				},
				{
					Name:  "inject new container - json format",
					Type:  appsapi.JSONPatchType,
					Value: `[{"op":"add","path": "/spec/containers/1","value":{"name":"injected-container","image":"redis:6.2.5"}}]`,
				},
			},
			want: []byte(`{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
					"name": "pod",
					"namespace": "test2",
					"labels": {
						"app": "nginx",
						"foo": "bar",
					},
				},
				"spec": {
					"containers": [{
							"name": "nginx",
							"image": "nginx:1.20.1"
						},
						{
							"name": "injected-container",
							"image": "redis:6.2.5"
						}
					]
				}
			}`),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := applyOverrides(tt.original, tt.overrides)
			if err != nil {
				t.Errorf("applyOverrides() error = %v", err)
				return
			}

			gotObj := &unstructured.Unstructured{Object: map[string]interface{}{}}
			wantObj := &unstructured.Unstructured{Object: map[string]interface{}{}}
			if err := yaml.Unmarshal(got, &gotObj); err != nil {
				t.Fatalf("error decoding: %v", err)
			}
			if err := yaml.Unmarshal(tt.want, &wantObj); err != nil {
				t.Fatalf("error decoding: %v", err)
			}

			if !reflect.DeepEqual(gotObj, wantObj) {
				t.Errorf("applyOverrides() got %s, want %s", gotObj, wantObj)
			}
		})
	}
}
