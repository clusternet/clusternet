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

// Copied from k8s.io/kubernetes/pkg/util/taints/taints_test.go and modified
// Some unused functions are deleted as well

package taints

import (
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func TestAddOrUpdateTaint(t *testing.T) {
	taint := corev1.Taint{
		Key:    "foo",
		Value:  "bar",
		Effect: corev1.TaintEffectNoSchedule,
	}

	taintNew := corev1.Taint{
		Key:    "foo_1",
		Value:  "bar_1",
		Effect: corev1.TaintEffectNoSchedule,
	}

	taintUpdateValue := taint
	taintUpdateValue.Value = "bar_1"

	testcases := []struct {
		name           string
		cluster        *clusterapi.ManagedCluster
		taint          *corev1.Taint
		expectedUpdate bool
		expectedTaints []corev1.Taint
	}{
		{
			name:           "add a new taint",
			cluster:        &clusterapi.ManagedCluster{},
			taint:          &taint,
			expectedUpdate: true,
			expectedTaints: []corev1.Taint{taint},
		},
		{
			name: "add a unique taint",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{Taints: []corev1.Taint{taint}},
			},
			taint:          &taintNew,
			expectedUpdate: true,
			expectedTaints: []corev1.Taint{taint, taintNew},
		},
		{
			name: "add duplicate taint",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{Taints: []corev1.Taint{taint}},
			},
			taint:          &taint,
			expectedUpdate: false,
			expectedTaints: []corev1.Taint{taint},
		},
		{
			name: "update taint value",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{Taints: []corev1.Taint{taint}},
			},
			taint:          &taintUpdateValue,
			expectedUpdate: true,
			expectedTaints: []corev1.Taint{taintUpdateValue},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			newNode, updated, err := AddOrUpdateTaint(tc.cluster, tc.taint)
			if err != nil {
				t.Errorf("[%s] should not raise error but got %v", tc.name, err)
			}
			if updated != tc.expectedUpdate {
				t.Errorf("[%s] expected taints to not be updated", tc.name)
			}
			if diff := cmp.Diff(newNode.Spec.Taints, tc.expectedTaints); diff != "" {
				t.Errorf("Unexpected result (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestTaintExists(t *testing.T) {
	testingTaints := []corev1.Taint{
		{
			Key:    "foo_1",
			Value:  "bar_1",
			Effect: corev1.TaintEffectNoExecute,
		},
		{
			Key:    "foo_2",
			Value:  "bar_2",
			Effect: corev1.TaintEffectNoSchedule,
		},
	}

	cases := []struct {
		name           string
		taintToFind    *corev1.Taint
		expectedResult bool
	}{
		{
			name:           "taint exists",
			taintToFind:    &corev1.Taint{Key: "foo_1", Value: "bar_1", Effect: corev1.TaintEffectNoExecute},
			expectedResult: true,
		},
		{
			name:           "different key",
			taintToFind:    &corev1.Taint{Key: "no_such_key", Value: "bar_1", Effect: corev1.TaintEffectNoExecute},
			expectedResult: false,
		},
		{
			name:           "different effect",
			taintToFind:    &corev1.Taint{Key: "foo_1", Value: "bar_1", Effect: corev1.TaintEffectNoSchedule},
			expectedResult: false,
		},
	}

	for _, c := range cases {
		result := TaintExists(testingTaints, c.taintToFind)

		if result != c.expectedResult {
			t.Errorf("[%s] unexpected results: %v", c.name, result)
			continue
		}
	}
}

func TestTaintSetFilter(t *testing.T) {
	testTaint1 := corev1.Taint{
		Key:    "foo_1",
		Value:  "bar_1",
		Effect: corev1.TaintEffectNoExecute,
	}
	testTaint2 := corev1.Taint{
		Key:    "foo_2",
		Value:  "bar_2",
		Effect: corev1.TaintEffectNoSchedule,
	}

	testTaint3 := corev1.Taint{
		Key:    "foo_3",
		Value:  "bar_3",
		Effect: corev1.TaintEffectNoSchedule,
	}
	testTaints := []corev1.Taint{testTaint1, testTaint2, testTaint3}

	testcases := []struct {
		name           string
		fn             func(t *corev1.Taint) bool
		expectedTaints []corev1.Taint
	}{
		{
			name: "Filter out nothing",
			fn: func(t *corev1.Taint) bool {
				if t.Key == corev1.TaintNodeUnschedulable {
					return true
				}
				return false
			},
			expectedTaints: []corev1.Taint{},
		},
		{
			name: "Filter out a subset",
			fn: func(t *corev1.Taint) bool {
				if t.Effect == corev1.TaintEffectNoExecute {
					return true
				}
				return false
			},
			expectedTaints: []corev1.Taint{testTaint1},
		},
		{
			name:           "Filter out everything",
			fn:             func(t *corev1.Taint) bool { return true },
			expectedTaints: []corev1.Taint{testTaint1, testTaint2, testTaint3},
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			taintsAfterFilter := TaintSetFilter(testTaints, tc.fn)
			if diff := cmp.Diff(tc.expectedTaints, taintsAfterFilter); diff != "" {
				t.Errorf("Unexpected postFilterResult (-want, +got):\n%s", diff)
			}
		})
	}
}

func TestRemoveTaint(t *testing.T) {
	cases := []struct {
		name           string
		cluster        *clusterapi.ManagedCluster
		taintToRemove  *corev1.Taint
		expectedTaints []corev1.Taint
		expectedResult bool
	}{
		{
			name: "remove taint unsuccessfully",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					Taints: []corev1.Taint{
						{
							Key:    "foo",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			taintToRemove: &corev1.Taint{
				Key:    "foo_1",
				Effect: corev1.TaintEffectNoSchedule,
			},
			expectedTaints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expectedResult: false,
		},
		{
			name: "remove taint successfully",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					Taints: []corev1.Taint{
						{
							Key:    "foo",
							Effect: corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
			taintToRemove: &corev1.Taint{
				Key:    "foo",
				Effect: corev1.TaintEffectNoSchedule,
			},
			expectedTaints: []corev1.Taint{},
			expectedResult: true,
		},
		{
			name: "remove taint from cluster with no taint",
			cluster: &clusterapi.ManagedCluster{
				Spec: clusterapi.ManagedClusterSpec{
					Taints: []corev1.Taint{},
				},
			},
			taintToRemove: &corev1.Taint{
				Key:    "foo",
				Effect: corev1.TaintEffectNoSchedule,
			},
			expectedTaints: []corev1.Taint{},
			expectedResult: false,
		},
	}

	for _, c := range cases {
		newNode, result, err := RemoveTaint(c.cluster, c.taintToRemove)
		if err != nil {
			t.Errorf("[%s] should not raise error but got: %v", c.name, err)
		}
		if result != c.expectedResult {
			t.Errorf("[%s] should return %t, but got: %t", c.name, c.expectedResult, result)
		}
		if !reflect.DeepEqual(newNode.Spec.Taints, c.expectedTaints) {
			t.Errorf("[%s] the new cluster object should have taints %v, but got: %v", c.name, c.expectedTaints, newNode.Spec.Taints)
		}
	}
}

func TestDeleteTaint(t *testing.T) {
	cases := []struct {
		name           string
		taints         []corev1.Taint
		taintToDelete  *corev1.Taint
		expectedTaints []corev1.Taint
		expectedResult bool
	}{
		{
			name: "delete taint with different name",
			taints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			taintToDelete: &corev1.Taint{Key: "foo_1", Effect: corev1.TaintEffectNoSchedule},
			expectedTaints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expectedResult: false,
		},
		{
			name: "delete taint with different effect",
			taints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			taintToDelete: &corev1.Taint{Key: "foo", Effect: corev1.TaintEffectNoExecute},
			expectedTaints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			expectedResult: false,
		},
		{
			name: "delete taint successfully",
			taints: []corev1.Taint{
				{
					Key:    "foo",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
			taintToDelete:  &corev1.Taint{Key: "foo", Effect: corev1.TaintEffectNoSchedule},
			expectedTaints: []corev1.Taint{},
			expectedResult: true,
		},
		{
			name:           "delete taint from empty taint array",
			taints:         []corev1.Taint{},
			taintToDelete:  &corev1.Taint{Key: "foo", Effect: corev1.TaintEffectNoSchedule},
			expectedTaints: []corev1.Taint{},
			expectedResult: false,
		},
	}

	for _, c := range cases {
		taints, result := DeleteTaint(c.taints, c.taintToDelete)
		if result != c.expectedResult {
			t.Errorf("[%s] should return %t, but got: %t", c.name, c.expectedResult, result)
		}
		if !reflect.DeepEqual(taints, c.expectedTaints) {
			t.Errorf("[%s] the result taints should be %v, but got: %v", c.name, c.expectedTaints, taints)
		}
	}
}
