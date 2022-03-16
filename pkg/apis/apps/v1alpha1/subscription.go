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
	corev1 "k8s.io/api/core/v1"
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

	// If specified, the Subscription will be handled with specified SchedulingStrategy.
	// Otherwise, with generic SchedulingStrategy.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Enum=Replication;Dividing
	// +kubebuilder:default=Replication
	SchedulingStrategy SchedulingStrategyType `json:"schedulingStrategy,omitempty"`

	// Dividing scheduling config params. Present only if SchedulingStrategyType = Dividing.
	//
	// +optional
	DividingScheduling *DividingSchedulingStrategy `json:"dividingSchedulingStrategy,omitempty"`

	// Subscribers subscribes
	//
	// +required
	// +kubebuilder:validation:Required
	Subscribers []Subscriber `json:"subscribers"`

	// ClusterTolerations tolerates any matched taints of ManagedCluster.
	//
	// +optional
	ClusterTolerations []corev1.Toleration `json:"clusterTolerations,omitempty"`

	// Feeds
	//
	// +required
	// +kubebuilder:validation:Required
	Feeds []Feed `json:"feeds"`

	// Namespaced names of targeted clusters that Subscription binds to.
	//
	// +optional
	BindingClusters []string `json:"bindingClusters,omitempty"`
}

// SubscriptionStatus defines the observed state of Subscription
type SubscriptionStatus struct {
	// Namespaced names of targeted clusters that Subscription binds to.
	//
	// +optional
	// Deprecated: Will be moved into `SubscriptionSpec`.
	BindingClusters []string `json:"bindingClusters,omitempty"`

	// SpecHash calculates the hash value of current SubscriptionSpec.
	//
	// +optional
	SpecHash uint64 `json:"specHash,omitempty"`

	// Total number of Helm releases desired by this Subscription.
	//
	// +optional
	DesiredReleases int `json:"desiredReleases,omitempty"`

	// Total number of completed releases targeted by this Subscription.
	//
	// +optional
	CompletedReleases int `json:"completedReleases,omitempty"`
}

// Subscriber defines
type Subscriber struct {
	// ClusterAffinity is a label query over managed clusters by labels.
	//
	// +required
	// +kubebuilder:validation:Required
	ClusterAffinity *metav1.LabelSelector `json:"clusterAffinity"`

	// Static weight of subscriber when dividing replicas.
	// Present only for static divided scheduling.
	//
	// +optional
	// +kubebuilder:validation:Minimum=1
	Weight *int32 `json:"weight,omitempty"`
}

// Feed defines the resource to be selected.
type Feed struct {
	// Kind is a string value representing the REST resource this object represents.
	// In CamelCase.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Kind string `json:"kind"`

	// APIVersion defines the versioned schema of this representation of an object.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	APIVersion string `json:"apiVersion"`

	// Namespace of the target resource.
	//
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Name of the target resource.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Type=string
	Name string `json:"name"`

	// Number of desired pods in child clusters if necessary.
	// The indices are corresponding with the scheduled clusters.
	//
	// +optional
	Replicas []int32 `json:"replicas,omitempty"`
}

// DividingSchedulingStrategy describes how to divide replicas into target clusters.
type DividingSchedulingStrategy struct {
	// Type of dividing replica scheduling.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Static
	// +kubebuilder:validation:Type=string
	Type ReplicaDividingType `json:"type"`
}

type SchedulingStrategyType string

const (
	// ReplicaSchedulingStrategyType places and maintains a copy of this Subscription on each matched clusters.
	ReplicaSchedulingStrategyType SchedulingStrategyType = "Replication"

	// DividingSchedulingStrategyType divides the replicas of a Subscription to several matching clusters.
	DividingSchedulingStrategyType SchedulingStrategyType = "Dividing"
)

type ReplicaDividingType string

const (
	// StaticReplicaDividingType divides replicas by a fixed weight.
	StaticReplicaDividingType ReplicaDividingType = "Static"
)

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}
