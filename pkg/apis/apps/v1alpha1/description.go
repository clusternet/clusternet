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
	"k8s.io/apimachinery/pkg/runtime"
)

// Important: Run "make generated" to regenerate code after modifying this file

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=desc,categories=clusternet
// +kubebuilder:printcolumn:name="DEPLOYER",type=string,JSONPath=".spec.deployer"
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Description is the Schema for the resources to be installed
type Description struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DescriptionSpec   `json:"spec"`
	Status DescriptionStatus `json:"status,omitempty"`
}

// DescriptionSpec defines the spec of Description
type DescriptionSpec struct {
	// Deployer indicates the deployer for this Description
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Enum=Helm;Generic
	Deployer DescriptionDeployer `json:"deployer"`

	// Charts describe all the helm charts to be installed
	//
	// +optional
	Charts []ChartReference `json:"charts,omitempty"`

	// Raw is the underlying serialization of all objects.
	//
	// +optional
	Raw [][]byte `json:"raw,omitempty"`

	// ChartRaw is the underlying serialization of all helm chart objects.
	//
	// +optional
	ChartRaw [][]byte `json:"chartRaw,omitempty"`
}

// DescriptionStatus defines the observed state of Description
type DescriptionStatus struct {
	// Phase denotes the phase of Description
	// +optional
	// +kubebuilder:validation:Enum=Pending;Success;Failure;Installing;Upgrading;Uninstalling;Superseded;Unknown
	Phase DescriptionPhase `json:"phase,omitempty"`

	// Reason indicates the reason of DescriptionPhase
	// +optional
	Reason string `json:"reason,omitempty"`

	// ManifestStatuses contains a list of running statuses of manifests in DescriptionSpec.
	//
	// +optional
	ManifestStatuses []ManifestStatus `json:"manifestStatuses,omitempty"`
}

// ManifestStatus contains details for the current status of this feed.
type ManifestStatus struct {
	// Feed holds references to the resource.
	Feed `json:",inline"`

	// ObservedStatus reflects observed status of current feed.
	//
	// +optional
	// +kubebuilder:pruning:PreserveUnknownFields
	ObservedStatus runtime.RawExtension `json:"observedStatus,omitempty"`
}

type DescriptionDeployer string

const (
	DescriptionHelmDeployer    DescriptionDeployer = "Helm"
	DescriptionGenericDeployer DescriptionDeployer = "Generic"
)

type DescriptionPhase string

const (
	DescriptionPhaseSuccess DescriptionPhase = "Success"
	DescriptionPhaseFailure DescriptionPhase = "Failure"

	// The following is the helm-specific status
	// DescriptionPhaseInstalling indicates that an install operation is underway.
	DescriptionPhaseInstalling DescriptionPhase = "Installing"
	// DescriptionPhaseUpgrading indicates that an upgrade operation is underway.
	DescriptionPhaseUpgrading DescriptionPhase = "Upgrading"
	// DescriptionPhaseUninstalling indicates that a uninstall operation is underway.
	DescriptionPhaseUninstalling DescriptionPhase = "Uninstalling"
	// DescriptionPhaseSuperseded indicates that this release object is outdated and a newer one exists.
	DescriptionPhaseSuperseded DescriptionPhase = "Superseded"
	// DescriptionPhaseUnknown indicates that a release is in an uncertain state.
	DescriptionPhaseUnknown DescriptionPhase = "Unknown"
)

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DescriptionList contains a list of Description
type DescriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Description `json:"items"`
}

type ChartReference struct {
	// Namespace of the HelmChart.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Namespace string `json:"namespace"`

	// Name of the HelmChart.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Name string `json:"name"`
}
