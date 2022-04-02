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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "make generated" to regenerate code after modifying this file

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Namespaced",shortName=finv,categories=clusternet

// FeedInventory defines a group of feeds which correspond to a subscription.
type FeedInventory struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Feeds []FeedOrder `json:"feeds"`
}

// FeedOrder defines an abstract representation of a feed.
type FeedOrder struct {
	Feed `json:",inline"`

	// DesiredReplicas represents the desired replicas of the workload.
	//
	// +required
	// +kubebuilder:validation:Required
	DesiredReplicas int32 `json:"desiredReplicas"`

	// ReplicaRequirements describes the scheduling requirements for a new replica.
	//
	// +required
	// +kubebuilder:validation:Required
	ReplicaRequirements ReplicaRequirements `json:"replicaRequirements"`
}

// ReplicaRequirements describes the scheduling requirements for a new replica.
type ReplicaRequirements struct {
	// NodeSelector specifies hard node constraints that must be met for a new replica to fit on a node.
	// Selector which must match a node's labels for a new replica to be scheduled on that node.
	// +optional
	// +mapType=atomic
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations specifies the tolerations of a new replica.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity specifies the scheduling constraints of a new replica.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// Resources describes the compute resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// FeedInventoryList contains a list of FeedInventory.
type FeedInventoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []FeedInventory `json:"items"`
}
