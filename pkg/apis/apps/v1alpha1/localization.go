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
// +kubebuilder:resource:scope="Namespaced",shortName=loc;local,categories=clusternet
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Localization represents the override config for a group of resources.
type Localization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec LocalizationSpec `json:"spec"`
}

// LocalizationSpec defines the desired state of Localization
type LocalizationSpec struct {
	// OverridePolicy specifies the override policy for this Localization.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default=ApplyLater
	OverridePolicy OverridePolicy `json:"overridePolicy,omitempty"`

	// Overrides holds all the OverrideConfig.
	//
	// +optional
	Overrides []OverrideConfig `json:"overrides,omitempty"`

	// Priority is an integer defining the relative importance of this Localization compared to others. Lower
	// numbers are considered lower priority.
	//
	// +optional
	// +kubebuilder:validation:Maximum=1000
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=500
	Priority int32 `json:"priority,omitempty"`

	// Feed holds references to the objects the Localization applies to.
	//
	// +optional
	Feed `json:"feed,omitempty"`
}

type OverridePolicy string

const (
	// Apply overrides for all matched objects immediately, including those already populated
	ApplyNow OverridePolicy = "ApplyNow"

	// Apply overrides for all matched objects on next updates (including updates on Subscription,
	// Manifest, HelmChart, etc) or new created objects.
	ApplyLater OverridePolicy = "ApplyLater"
)

type OverrideType string

// Available values for OverrideType are: JSONPatch, StrategicMergePatch and HelmValues.
const (
	// JsonPatchOverride apply JSONPatch for all matched objects (excluding HelmChart).
	JSONPatchOverride OverrideType = "JSONPatch"

	// StrategicMergePatchOverride apply Strategic MergePatch for all matched objects (excluding HelmChart).
	StrategicMergePatchOverride OverrideType = "StrategicMergePatch"

	// HelmOverride apply Helm values for all matched HelmCharts.
	HelmOverride OverrideType = "Helm"
)

// OverrideConfig holds information that describes a override config.
type OverrideConfig struct {
	// Name indicate the OverrideConfig name.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// Value represents override value.
	//
	// +optional
	Value string `json:"value,omitempty"`

	// Type specifies the override type for override value.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default=Helm
	Type OverrideType `json:"type,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LocalizationList contains a list of Localization
type LocalizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Localization `json:"items"`
}
