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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
)

func UpdateConditions(status *clusterapi.ManagedClusterStatus, newConditions []metav1.Condition) (hasChanged bool) {
	if status.Conditions == nil {
		status.Conditions = []metav1.Condition{}
	}
	for _, newCondition := range newConditions {
		oldCondition := GetCondition(status.Conditions, newCondition.Type)
		// only new condition of this type exists, add to the list
		if oldCondition == nil {
			status.Conditions = append(status.Conditions, newCondition)
			hasChanged = true
		} else if oldCondition.Status != newCondition.Status || oldCondition.Message != newCondition.Message || oldCondition.Reason != newCondition.Reason {
			// old condition needs to be updated
			if oldCondition.Status != newCondition.Status {
				oldCondition.LastTransitionTime = metav1.Now()
			}
			oldCondition.Type = newCondition.Type
			oldCondition.Status = newCondition.Status
			oldCondition.Reason = newCondition.Reason
			oldCondition.Message = newCondition.Message
			hasChanged = true
		} else if oldCondition.Type == clusterapi.ClusterReady {
			// here all the fields in condition are the same, except LastTransitionTime
			// we do need to report heartbeats
			oldCondition.LastTransitionTime = newCondition.LastTransitionTime
			hasChanged = true
		}
	}
	return
}

func GetCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &(conditions[i])
		}
	}
	return nil
}
