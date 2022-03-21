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
	"helm.sh/helm/v3/pkg/release"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Important: Run "make generated" to regenerate code after modifying this file

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=chart;charts,categories=clusternet
// +kubebuilder:printcolumn:name="CHART",type=string,JSONPath=`.spec.chart`,description="The helm chart name"
// +kubebuilder:printcolumn:name="VERSION",type=string,JSONPath=`.spec.version`,description="The helm chart version"
// +kubebuilder:printcolumn:name="REPO",type=string,JSONPath=`.spec.repo`,description="The helm repo url"
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=".status.phase",description="The helm chart status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// HelmChart is the Schema for the helm chart
type HelmChart struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmChartSpec   `json:"spec"`
	Status HelmChartStatus `json:"status,omitempty"`
}

// HelmChartSpec defines the spec of HelmChart
type HelmChartSpec struct {
	HelmOptions `json:",inline"`

	// TargetNamespace specifies the namespace to install this HelmChart
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	TargetNamespace string `json:"targetNamespace"`
}

// HelmChartStatus defines the observed state of HelmChart
type HelmChartStatus struct {
	// Phase denotes the phase of HelmChart
	//
	// +optional
	// +kubebuilder:validation:Enum=Found;NotFound
	Phase HelmChartPhase `json:"phase,omitempty"`

	// Reason indicates the reason of HelmChartPhase
	//
	// +optional
	Reason string `json:"reason,omitempty"`
}

type HelmChartPhase string

const (
	HelmChartFound    HelmChartPhase = "Found"
	HelmChartNotFound HelmChartPhase = "NotFound"
)

type HelmOptions struct {
	// a Helm Repository to be used.
	// OCI-based registries are also supported.
	// For example, https://charts.bitnami.com/bitnami or oci://localhost:5000/helm-charts
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern=`^(http|https|oci)?://(?:[a-zA-Z]|[0-9]|[$-_@.&+]|[!*\(\),]|(?:%[0-9a-fA-F][0-9a-fA-F]))+$`
	Repository string `json:"repo"`

	// ChartPullSecret is the name of the secret that contains the auth information for the chart repository.
	//
	// +optional
	ChartPullSecret ChartPullSecret `json:"chartPullSecret,omitempty"`

	// Chart is the name of a Helm Chart in the Repository.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Chart string `json:"chart"`

	// ChartVersion is the version of the chart to be deployed.
	// It will be defaulted with current latest version if empty.
	//
	// +optional
	ChartVersion string `json:"version,omitempty"`
}

// ChartPullSecret is the name of the secret that contains the auth information for the chart repository.
type ChartPullSecret struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HelmChartList contains a list of HelmChart
type HelmChartList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmChart `json:"items"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Namespaced",shortName=hr,categories=clusternet
// +kubebuilder:printcolumn:name="CHART",type=string,JSONPath=`.spec.chart`,description="The helm chart name"
// +kubebuilder:printcolumn:name="VERSION",type=string,JSONPath=`.spec.version`,description="The helm chart version"
// +kubebuilder:printcolumn:name="REPO",type=string,JSONPath=`.spec.repo`,description="The helm repo url"
// +kubebuilder:printcolumn:name="STATUS",type=string,JSONPath=".status.phase",description="The helm release status"
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// HelmRelease is the Schema for the helm release
type HelmRelease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   HelmReleaseSpec   `json:"spec"`
	Status HelmReleaseStatus `json:"status,omitempty"`
}

// HelmReleaseSpec defines the spec of HelmRelease
type HelmReleaseSpec struct {
	HelmOptions `json:",inline"`

	// ReleaseName specifies the desired release name in child cluster.
	// If nil, the default release name will be in the format of "{Description Name}-{HelmChart Namespace}-{HelmChart Name}"
	//
	// +optional
	// +kubebuilder:validation:Type=string
	ReleaseName *string `json:"releaseName,omitempty"`

	// TargetNamespace specifies the namespace to install the chart
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	TargetNamespace string `json:"targetNamespace"`
}

// HelmReleaseStatus defines the observed state of HelmRelease
type HelmReleaseStatus struct {
	// FirstDeployed is when the release was first deployed.
	//
	// +optional
	FirstDeployed string `json:"firstDeployed,omitempty"`

	// LastDeployed is when the release was last deployed.
	//
	// +optional
	LastDeployed string `json:"lastDeployed,omitempty"`

	// Description is human-friendly "log entry" about this release.
	//
	// +optional
	Description string `json:"description,omitempty"`

	// Phase is the current state of the release
	Phase release.Status `json:"phase,omitempty"`

	// Contains the rendered templates/NOTES.txt if available
	//
	// +optional
	Notes string `json:"notes,omitempty"`

	// Version is an int which represents the revision of the release.
	//
	// +optional
	Version int `json:"version,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// HelmReleaseList contains a list of HelmRelease
type HelmReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []HelmRelease `json:"items"`
}
