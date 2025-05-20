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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func TestResourceNeedResync(t *testing.T) {
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
	barU := &unstructured.Unstructured{
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
			"status": map[string]interface{}{
				"availableReplicas":  2,
				"observedGeneration": 1,
			},
		},
	}
	barV := &unstructured.Unstructured{
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
			"status": map[string]interface{}{
				"availableReplicas":  2,
				"observedGeneration": 1000,
			},
		},
	}

	tests := []struct {
		label     string // Test name
		x         *unstructured.Unstructured
		y         *unstructured.Unstructured
		wantSync  bool   // Whether the inputs are equal
		reason    string // The reason for the expected outcome
		ignoreAdd bool   // whether or not ignore add action.
	}{
		{
			label:     "fields-populated",
			x:         barX,
			y:         barY,
			wantSync:  false,
			reason:    "won't re-sync because fields are auto populated",
			ignoreAdd: true,
		},
		{
			label:     "fields-removed",
			x:         barY,
			y:         barX,
			wantSync:  true,
			reason:    "should re-sync because fields are removed",
			ignoreAdd: false,
		},
		{
			label:     "fields-changed",
			x:         barX,
			y:         barZ,
			wantSync:  true,
			reason:    "should re-sync because fields get changed",
			ignoreAdd: false,
		},
		{
			label:     "fields-added",
			x:         barX,
			y:         barY,
			wantSync:  true,
			reason:    "should re-sync because add action are not ignored",
			ignoreAdd: false,
		},
		{
			label:     "section-ignored",
			x:         barX,
			y:         barU,
			wantSync:  false,
			reason:    "won't re-sync because status section is ignored",
			ignoreAdd: true,
		},
		{
			label:     "section-ignored",
			x:         barU,
			y:         barV,
			wantSync:  false,
			reason:    "won't re-sync because status section is ignored",
			ignoreAdd: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			var gotEqual bool
			func() {
				gotEqual = ResourceNeedResync(tt.x, tt.y, tt.ignoreAdd)
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

func TestClusterHasReadyCondition(t *testing.T) {
	tests := []struct {
		name         string
		mc           *clusterapi.ManagedCluster
		clusterReady bool
	}{
		{
			mc: &clusterapi.ManagedCluster{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       clusterapi.ManagedClusterSpec{},
			},
			clusterReady: false,
		},
		{
			mc: &clusterapi.ManagedCluster{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       clusterapi.ManagedClusterSpec{},
				Status: clusterapi.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							LastTransitionTime: metav1.Now(),
							Message:            "managed cluster is ready.",
							Reason:             "ManagedClusterReady",
							Status:             metav1.ConditionTrue,
							Type:               clusterapi.ClusterReady,
						},
					},
				},
			},
			clusterReady: true,
		},
		{
			mc: &clusterapi.ManagedCluster{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       clusterapi.ManagedClusterSpec{},
				Status: clusterapi.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							LastTransitionTime: metav1.Now(),
							Message:            "managed cluster is in unknown status",
							Reason:             "ManagedClusterUnknown",
							Status:             metav1.ConditionUnknown,
							Type:               clusterapi.ClusterReady,
						},
					},
				},
			},
			clusterReady: false,
		},
		{
			mc: &clusterapi.ManagedCluster{
				TypeMeta:   metav1.TypeMeta{},
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       clusterapi.ManagedClusterSpec{},
				Status: clusterapi.ManagedClusterStatus{
					Conditions: []metav1.Condition{
						{
							LastTransitionTime: metav1.Now(),
							Message:            "managed cluster is not ready",
							Reason:             "ManagedClusterNotReady",
							Status:             metav1.ConditionFalse,
							Type:               clusterapi.ClusterReady,
						},
					},
				},
			},
			clusterReady: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if isReady := ClusterHasReadyCondition(tt.mc); isReady != tt.clusterReady {
				t.Errorf("ClusterHasReadyCondition() = %v, want %v", isReady, tt.clusterReady)
			}
		})
	}
}
