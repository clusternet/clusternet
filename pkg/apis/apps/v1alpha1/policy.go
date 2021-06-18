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
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=annc,categories=clusternet
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Announcement represents the policy that install a group of resources to one or more clusters.
type Announcement struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AnnouncementSpec   `json:"spec"`
	Status AnnouncementStatus `json:"status,omitempty"`
}

// AnnouncementSpec defines the desired state of Announcement
type AnnouncementSpec struct {
	// ClusterAffinity is a label query over managed clusters by labels.
	//
	// +required
	// +kubebuilder:validation:Required
	ClusterAffinity *metav1.LabelSelector `json:"clusterAffinity"`

	// ChartSelectors select all matching HelmChart(s) in current namespace
	//
	// +required
	// +kubebuilder:validation:Required
	ChartSelectors []ChartSelector `json:"chartSelectors"`
}

// AnnouncementStatus defines the observed state of Announcement
type AnnouncementStatus struct {
	// Total number of Helm releases desired by this Announcement.
	//
	// +optional
	DesiredReleases int32 `json:"desiredReleases,omitempty"`

	// Total number of completed releases targeted by this deployment.
	//
	// +optional
	CompletedReleases int32 `json:"completedReleases,omitempty"`
}

// ChartSelector selects the HelmChart to be deployed
type ChartSelector struct {
	// Name of the HelmChart.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// A label query over a set of HelmChart.
	// LabelSelector will be ignored if Name is not empty.
	//
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AnnouncementList contains a list of Announcement
type AnnouncementList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Announcement `json:"items"`
}
