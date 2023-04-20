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

package clusterlifecycle

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func TestGetClusterReadyCondition(t *testing.T) {
	heartbeatFrequencySeconds := int64(60)

	tests := []struct {
		name  string
		input *clusterapi.ManagedCluster
		want  *metav1.Condition
	}{
		{
			name: "new cluster with zero lastObservedTime and currentTimestamp is before CreationTimestamp + gracePeriod",
			input: &clusterapi.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: metav1.Now().Add(-60 * time.Second)},
				},
				Status: clusterapi.ManagedClusterStatus{
					Readyz:                    true,
					Livez:                     true,
					HeartbeatFrequencySeconds: &heartbeatFrequencySeconds,
				},
			},
			want: nil,
		},
		{
			name: "new cluster with zero lastObservedTime and currentTimestamp is after CreationTimestamp + gracePeriod",
			input: &clusterapi.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Time{Time: metav1.Now().Add(-190 * time.Second)},
				},
				Status: clusterapi.ManagedClusterStatus{
					Readyz:                    true,
					Livez:                     true,
					HeartbeatFrequencySeconds: &heartbeatFrequencySeconds,
				},
			},
			want: &metav1.Condition{
				Type:    clusterapi.ClusterReady,
				Status:  metav1.ConditionUnknown,
				Reason:  "ClusterStatusNeverUpdated",
				Message: "Clusternet agent never posted cluster status.",
			},
		},
		{
			name: "currentTimestamp is after LastObservedTime + gracePeriod",
			input: &clusterapi.ManagedCluster{
				Status: clusterapi.ManagedClusterStatus{
					LastObservedTime:          metav1.Time{Time: metav1.Now().Add(-4 * time.Minute)},
					Readyz:                    true,
					Livez:                     true,
					HeartbeatFrequencySeconds: &heartbeatFrequencySeconds,
				},
			},
			want: &metav1.Condition{
				Type:    clusterapi.ClusterReady,
				Status:  metav1.ConditionUnknown,
				Reason:  "ClusterStatusUnknown",
				Message: "Clusternet agent stopped posting cluster status.",
			},
		},
		{
			name: "currentTimestamp is before LastObservedTime + gracePeriod",
			input: &clusterapi.ManagedCluster{

				Status: clusterapi.ManagedClusterStatus{
					LastObservedTime:          metav1.Time{Time: metav1.Now().Add(-30 * time.Second)},
					Readyz:                    true,
					Livez:                     true,
					HeartbeatFrequencySeconds: &heartbeatFrequencySeconds,
				},
			},
			want: nil,
		},
		{
			name: "cluster HeartbeatFrequencySeconds is nil",
			input: &clusterapi.ManagedCluster{
				Status: clusterapi.ManagedClusterStatus{
					LastObservedTime: metav1.Time{Time: metav1.Now().Add(-10 * time.Minute)},
					Readyz:           true,
					Livez:            true,
				},
			},
			want: &metav1.Condition{
				Type:    clusterapi.ClusterReady,
				Status:  metav1.ConditionUnknown,
				Reason:  "ClusterStatusUnknown",
				Message: "Clusternet agent stopped posting cluster status.",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ClusterConditionAsserts(t, tt.want, generateClusterReadyCondition(tt.input))
		})
	}
}

func ClusterConditionAsserts(t *testing.T, want, got *metav1.Condition) {
	if want == nil && got == nil {
		return
	}
	if want == nil && got != nil {
		t.Errorf("ClusterConditionAsserts: got %v, want nil", got)
		return
	}

	if want != nil && got == nil {
		t.Errorf("ClusterConditionAsserts: got nil, want %v", want)
		return
	}

	if want.Status != got.Status {
		t.Errorf("ClusterConditionAsserts: got status %s, want %s", got.Status, want.Status)
	}

	if want.Type != got.Type {
		t.Errorf("ClusterConditionAsserts: got type %s, want %s", got.Type, want.Type)
	}

	if want.Reason != got.Reason {
		t.Errorf("ClusterConditionAsserts: got reason %s, want %s", got.Reason, want.Reason)
	}

	if want.Message != got.Message {
		t.Errorf("ClusterConditionAsserts: got message %s, want %s", got.Message, want.Message)
	}
}
