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
// +kubebuilder:resource:scope="Namespaced",shortName=sub;subs,categories=clusternet
// +kubebuilder:printcolumn:name="AGE",type="date",JSONPath=".metadata.creationTimestamp"

// Subscription represents the policy that install a group of resources to one or more clusters.
type Subscription struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SubscriptionSpec   `json:"spec"`
	Status SubscriptionStatus `json:"status,omitempty"`
}

// SubscriptionSpec defines the desired state of Subscription
type SubscriptionSpec struct {
	// If specified, the Subscription will be handled by specified scheduler.
	// If not specified, the Subscription will be handled by default scheduler.
	//
	// +optional
	// +kubebuilder:default=default
	SchedulerName string `json:"schedulerName,omitempty"`

	// Subscribers subscribes
	//
	// +required
	// +kubebuilder:validation:Required
	Subscribers []Subscriber `json:"subscribers"`

	// Feeds
	//
	// +required
	// +kubebuilder:validation:Required
	Feeds []Feed `json:"feeds"`
}

// SubscriptionStatus defines the observed state of Subscription
type SubscriptionStatus struct {
	// Total number of Helm releases desired by this Subscription.
	//
	// +optional
	DesiredReleases int32 `json:"desiredReleases,omitempty"`

	// Total number of completed releases targeted by this deployment.
	//
	// +optional
	CompletedReleases int32 `json:"completedReleases,omitempty"`
}

// Subscriber defines
type Subscriber struct {
	// ClusterAffinity is a label query over managed clusters by labels.
	//
	// +required
	// +kubebuilder:validation:Required
	ClusterAffinity *metav1.LabelSelector `json:"clusterAffinity"`
}

// Feed defines the resource to be selected.
type Feed struct {
	// Kind is a string value representing the REST resource this object represents.
	// In CamelCase.
	//
	// +required
	// +kubebuilder:validation:Required
	Kind string `json:"kind"`

	// APIVersion defines the versioned schema of this representation of an object.
	//
	// +required
	// +kubebuilder:validation:Required
	APIVersion string `json:"apiVersion"`

	// Namespace of the target resource.
	// Default to use the same Namespace of Subscription.
	//
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the target resource.
	// Either Name or FeedSelector should be set.
	//
	// +optional
	Name string `json:"name,omitempty"`

	// FeedSelector selects all matching resources.
	// FeedSelector will be ignored if Name is specified.
	//
	// +optional
	FeedSelector *metav1.LabelSelector `json:"feedSelector,omitempty"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}
