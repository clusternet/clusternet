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

// Copied from k8s.io/kubernetes/pkg/util/taints/taints.go and modified
// Some unused functions are deleted as well

// package taints implements utilities for working with taints

package taints

import (
	"reflect"

	v1 "k8s.io/api/core/v1"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

// DeleteTaint removes all the taints that have the same key and effect to given taintToDelete.
func DeleteTaint(taints []v1.Taint, taintToDelete *v1.Taint) ([]v1.Taint, bool) {
	newTaints := []v1.Taint{}
	deleted := false
	for i := range taints {
		if taintToDelete.MatchTaint(&taints[i]) {
			deleted = true
			continue
		}
		newTaints = append(newTaints, taints[i])
	}
	return newTaints, deleted
}

// RemoveTaint tries to remove a taint from annotations list.
// Returns a new copy of updated Cluster and true if something was updated false otherwise.
func RemoveTaint(cluster *clusterapi.ManagedCluster, taint *v1.Taint) (*clusterapi.ManagedCluster, bool, error) {
	newCluster := cluster.DeepCopy()
	clusterTaints := newCluster.Spec.Taints
	if len(clusterTaints) == 0 {
		return newCluster, false, nil
	}

	if !TaintExists(clusterTaints, taint) {
		return newCluster, false, nil
	}

	newTaints, _ := DeleteTaint(clusterTaints, taint)
	newCluster.Spec.Taints = newTaints
	return newCluster, true, nil
}

// AddOrUpdateTaint tries to add a taint to annotations list.
// Returns a new copy of updated Cluster and true if something was updated false otherwise.
func AddOrUpdateTaint(cluster *clusterapi.ManagedCluster, taint *v1.Taint) (*clusterapi.ManagedCluster, bool, error) {
	newCluster := cluster.DeepCopy()
	clusterTaints := newCluster.Spec.Taints

	var newTaints []v1.Taint
	updated := false
	for i := range clusterTaints {
		if taint.MatchTaint(&clusterTaints[i]) {
			if reflect.DeepEqual(*taint, clusterTaints[i]) {
				return newCluster, false, nil
			}
			newTaints = append(newTaints, *taint)
			updated = true
			continue
		}

		newTaints = append(newTaints, clusterTaints[i])
	}

	if !updated {
		newTaints = append(newTaints, *taint)
	}

	newCluster.Spec.Taints = newTaints
	return newCluster, true, nil
}

// TaintExists checks if the given taint exists in list of taints. Returns true if exists false otherwise.
func TaintExists(taints []v1.Taint, taintToFind *v1.Taint) bool {
	for _, taint := range taints {
		if taint.MatchTaint(taintToFind) {
			return true
		}
	}
	return false
}

// TaintSetDiff finds the difference between two taint slices and
// returns all new and removed elements of the new slice relative to the old slice.
// for example:
// input: taintsNew=[a b] taintsOld=[a c]
// output: taintsToAdd=[b] taintsToRemove=[c]
func TaintSetDiff(taintsNew, taintsOld []v1.Taint) (taintsToAdd []*v1.Taint, taintsToRemove []*v1.Taint) {
	for _, taint := range taintsNew {
		if !TaintExists(taintsOld, &taint) {
			t := taint
			taintsToAdd = append(taintsToAdd, &t)
		}
	}

	for _, taint := range taintsOld {
		if !TaintExists(taintsNew, &taint) {
			t := taint
			taintsToRemove = append(taintsToRemove, &t)
		}
	}

	return
}

// TaintSetFilter filters from the taint slice according to the passed fn function to get the filtered taint slice.
func TaintSetFilter(taints []v1.Taint, fn func(*v1.Taint) bool) []v1.Taint {
	res := []v1.Taint{}

	for _, taint := range taints {
		if fn(&taint) {
			res = append(res, taint)
		}
	}

	return res
}
