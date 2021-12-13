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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestEqualLeftFieldEmpty(t *testing.T) {

	barX := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"externalTrafficPolicy": "Cluster",
				"ipFamilies": []string{
					"IPv4",
				},
				"ipFamilyPolicy": "SingleStack",
				"ports": []interface{}{
					map[string]interface{}{
						"name":       "tcp-80-80",
						"port":       int64(80),
						"protocol":   "TCP",
						"targetPort": int64(80),
					},
					map[string]interface{}{
						"name":       "tcp-443-443",
						"port":       int64(443),
						"protocol":   "TCP",
						"targetPort": int64(443),
					},
				},
			},
		},
	}
	barY := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"clusterIP": "10.98.177.115", // add this field
				"clusterIPs": []string{ // add this field
					"10.98.177.115",
				},
				"externalTrafficPolicy": "Cluster",
				"ipFamilies": []string{
					"IPv4",
				},
				"ipFamilyPolicy": "SingleStack",
				"ports": []interface{}{
					map[string]interface{}{
						"name":       "tcp-80-80",
						"port":       int64(80),
						"protocol":   "TCP",
						"targetPort": int64(80),
					},
					map[string]interface{}{
						"name":       "tcp-443-443",
						"port":       int64(443),
						"protocol":   "TCP",
						"targetPort": int64(443),
					},
				},
			},
		},
	}
	barZ := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"spec": map[string]interface{}{
				"externalTrafficPolicy": "Cluster",
				"ipFamilies": []string{
					"IPv6", // here is the difference
				},
				"ipFamilyPolicy": "SingleStack",
				"ports": []interface{}{
					map[string]interface{}{
						"name":       "tcp-80-80",
						"port":       int64(80),
						"protocol":   "TCP",
						"targetPort": int64(80),
					},
					map[string]interface{}{
						"name":       "tcp-443-443",
						"port":       int64(443),
						"protocol":   "TCP",
						"targetPort": int64(443),
					},
				},
			},
		},
	}

	tests := []struct {
		label    string // Test name
		x        *unstructured.Unstructured
		y        *unstructured.Unstructured
		wantSync bool   // Whether the inputs are equal
		reason   string // The reason for the expected outcome
	}{{
		label:    "RightsideHasMoreField",
		x:        barX,
		y:        barY,
		wantSync: false,
		reason:   "won't sync because right side has more field",
	},
		{
			label:    "RightsideHasDiffValue",
			x:        barX,
			y:        barZ,
			wantSync: true,
			reason:   "will sync because right side has different value on the same field",
		},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			var gotEqual bool
			func() {
				gotEqual = ResourceNeedResync(tt.x, tt.y)
			}()
			switch {
			case tt.reason == "":
				t.Errorf("reason must be provided")
			case gotEqual != tt.wantSync:
				t.Errorf("Equal = %v, want %v\nreason: %v", gotEqual, tt.wantSync, tt.reason)
			}
		})
	}
}
