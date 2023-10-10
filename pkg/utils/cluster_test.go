/*
Copyright 2023 The Clusternet Authors.

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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func TestUpdateConditions(t *testing.T) {
	tests := []struct {
		name               string
		status             *clusterapi.ManagedClusterStatus
		newConditions      []metav1.Condition
		expectedConditions []metav1.Condition
		wantHasChanged     bool
	}{
		{
			name: "starting from empty",
			status: &clusterapi.ManagedClusterStatus{
				Conditions: []metav1.Condition{},
			},
			newConditions: []metav1.Condition{
				{Type: "foo", Status: metav1.ConditionTrue},
			},
			expectedConditions: []metav1.Condition{
				{Type: "foo", Status: metav1.ConditionTrue},
			},
			wantHasChanged: true,
		},
		{
			name: "append new conditions",
			status: &clusterapi.ManagedClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "foo", Status: metav1.ConditionTrue},
					{Type: "bar", Status: metav1.ConditionTrue},
				},
			},
			newConditions: []metav1.Condition{
				{Type: "qux", Status: metav1.ConditionTrue},
			},
			expectedConditions: []metav1.Condition{
				{Type: "foo", Status: metav1.ConditionTrue},
				{Type: "bar", Status: metav1.ConditionTrue},
				{Type: "qux", Status: metav1.ConditionTrue},
			},
			wantHasChanged: true,
		},
		{
			name: "no updates",
			status: &clusterapi.ManagedClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "foo", Status: metav1.ConditionTrue},
					{Type: "bar", Status: metav1.ConditionTrue},
					{Type: "qux", Status: metav1.ConditionTrue},
				},
			},
			newConditions: []metav1.Condition{
				{Type: "bar", Status: metav1.ConditionTrue},
			},
			expectedConditions: []metav1.Condition{
				{Type: "foo", Status: metav1.ConditionTrue},
				{Type: "bar", Status: metav1.ConditionTrue},
				{Type: "qux", Status: metav1.ConditionTrue},
			},
			wantHasChanged: false,
		},
		{
			name: "update existing conditions with different orders",
			status: &clusterapi.ManagedClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "foo", Status: metav1.ConditionTrue, Message: "success"},
					{Type: "bar", Status: metav1.ConditionTrue},
					{Type: "qux", Status: metav1.ConditionTrue},
				},
			},
			newConditions: []metav1.Condition{
				{Type: "qux", Status: metav1.ConditionFalse, Message: "this is a failure message"},
				{Type: "foo", Status: metav1.ConditionFalse, Message: "this is a failure message"},
			},
			expectedConditions: []metav1.Condition{
				{Type: "foo", Status: metav1.ConditionFalse, Message: "this is a failure message"},
				{Type: "bar", Status: metav1.ConditionTrue},
				{Type: "qux", Status: metav1.ConditionFalse, Message: "this is a failure message"},
			},
			wantHasChanged: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotHasChanged := UpdateConditions(tt.status, tt.newConditions); gotHasChanged != tt.wantHasChanged {
				t.Errorf("UpdateConditions got %v, want %v", gotHasChanged, tt.wantHasChanged)
			}

			actuals := tt.status.Conditions
			if len(actuals) != len(tt.expectedConditions) {
				t.Fatal(actuals)
			}
			for i := range actuals {
				actual := actuals[i]
				expected := tt.expectedConditions[i]
				expected.LastTransitionTime = actual.LastTransitionTime
				if !reflect.DeepEqual(expected, actual) {
					t.Errorf("Below conditions are not matched\n\t%+v\n\t%+v", actual, expected)
				}
			}
		})
	}
}
