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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "make generated" to regenerate code after modifying this file

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:scope="Cluster",shortName=glob;global,categories=clusternet
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Globalization represents the cluster-scoped override config for a group of resources.
type Globalization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GlobalizationSpec `json:"spec"`
}

// GlobalizationSpec defines the desired state of Globalization
type GlobalizationSpec struct {
	// OverridePolicy specifies the override policy for this Globalization.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default=ApplyLater
	OverridePolicy OverridePolicy `json:"overridePolicy,omitempty"`

	// ClusterAffinity is a label query over managed clusters by labels.
	// If no labels are specified, all clusters will be selected.
	//
	// +optional
	ClusterAffinity *metav1.LabelSelector `json:"clusterAffinity,omitempty"`

	// Overrides holds all the OverrideConfig.
	//
	// +optional
	Overrides []OverrideConfig `json:"overrides,omitempty"`

	// Priority is an integer defining the relative importance of this Globalization compared to others. Lower
	// numbers are considered lower priority.
	//
	// +optional
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=500
	Priority int32 `json:"priority,omitempty"`

	// Feed holds references to the objects the Globalization applies to.
	//
	// +optional
	Feed `json:"feed,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// GlobalizationList contains a list of Globalization
type GlobalizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Globalization `json:"items"`
}
