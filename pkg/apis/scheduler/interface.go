/*
Copyright 2022 The Clusternet Authors.

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

package scheduler

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

// EstimatorProvider is an interface that provides replicas estimations.
type EstimatorProvider interface {
	// MaxAcceptableReplicas indicates the maximum acceptable replicas that the cluster could admit.
	// It returns a map of label to maximum acceptable replicas with this constraint. Here the label constraint
	// could be topology constraints, such as
	// {
	//    "topology.kubernetes.io/zone=zone1,topology.kubernetes.io/region=region1": 3,
	//    "topology.kubernetes.io/zone=zone2,topology.kubernetes.io/region=region1": 5,
	// }.
	MaxAcceptableReplicas(ctx context.Context, requirements v1alpha1.ReplicaRequirements) (map[string]int32, error)

	// UnschedulableReplicas returns current unschedulable replicas.
	UnschedulableReplicas(ctx context.Context, gvk metav1.GroupVersionKind, namespacedName string,
		labelSelector map[string]string) (int32, error)

	// TODO: resource reservations
}
