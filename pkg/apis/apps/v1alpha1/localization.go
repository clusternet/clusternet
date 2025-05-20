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
	// +kubebuilder:validation:Enum=ApplyNow;ApplyLater
	// +kubebuilder:default=ApplyLater
	OverridePolicy OverridePolicy `json:"overridePolicy,omitempty"`

	// Overrides holds all the OverrideConfig.
	//
	// +optional
	Overrides []OverrideConfig `json:"overrides,omitempty"`

	// Priority is an integer defining the relative importance of this Localization compared to others.
	// Lower numbers are considered lower priority.
	// And these Localization(s) will be applied by order from lower priority to higher.
	// That means override values in lower Localization will be overridden by those in higher Localization.
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

const (
	// HelmType applies Helm values for all matched HelmCharts.
	// Note: HelmType only works with HelmChart(s).
	HelmType OverrideType = "Helm"

	// JSONPatchType applies a json patch for all matched objects.
	// Note: JSONPatchType does not work with HelmChart(s).
	JSONPatchType OverrideType = "JSONPatch"

	// MergePatchType applies a json merge patch for all matched objects.
	// Note: MergePatchType does not work with HelmChart(s).
	MergePatchType OverrideType = "MergePatch"

	// StrategicMergePatchType won't be supported, since `patchStrategy`
	// and `patchMergeKey` can not be retrieved.
)

// OverrideConfig holds information that describes a override config.
type OverrideConfig struct {
	// Name indicate the OverrideConfig name.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// Value represents override value.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Value string `json:"value"`

	// Type specifies the override type for override value.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Enum=Helm;JSONPatch;MergePatch
	Type OverrideType `json:"type"`

	// OverrideChart indicates whether the override value for the HelmChart CR.
	//
	// +optional
	OverrideChart bool `json:"overrideChart,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// LocalizationList contains a list of Localization
type LocalizationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Localization `json:"items"`
}
