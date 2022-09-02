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
	"k8s.io/apimachinery/pkg/types"
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

	// Dividing scheduling config params. Present only if SchedulingStrategy = Dividing.
	//
	// +optional
	DividingScheduling *DividingScheduling `json:"dividingScheduling,omitempty"`

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
}

// SubscriptionStatus defines the observed state of Subscription
type SubscriptionStatus struct {
	// Namespaced names of targeted clusters that Subscription binds to.
	//
	// +optional
	BindingClusters []string `json:"bindingClusters,omitempty"`

	// Desired replicas of targeted clusters for each feed.
	//
	// +optional
	Replicas map[string][]int32 `json:"replicas,omitempty"`

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

	// AggregatedStatuses shows the aggregated statuses of feeds that are running in each child cluster.
	//
	// +optional
	AggregatedStatuses []AggregatedStatus `json:"aggregatedStatuses,omitempty"`
}

// AggregatedStatus contains aggregated status of current feed.
type AggregatedStatus struct {
	// Feed holds references to the resource.
	Feed `json:",inline"`

	// FeedStatusSummary aggregates the feed statuses from each child cluster.
	//
	// +optional
	FeedStatusSummary FeedStatus `json:"feedStatusSummary,omitempty"`

	// FeedStatusDetails shows the feed statuses in each child cluster.
	//
	// +optional
	FeedStatusDetails []FeedStatusPerCluster `json:"feedStatusDetails,omitempty"`
}

// FeedStatusPerCluster shows the feed status running in current cluster.
type FeedStatusPerCluster struct {
	// ClusterID indicates the id of current cluster.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}"
	ClusterID types.UID `json:"clusterId,omitempty"`

	// ClusterName is the cluster name.
	//
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:MaxLength=30
	// +kubebuilder:validation:Pattern="[a-z0-9]([-a-z0-9]*[a-z0-9])?([a-z0-9]([-a-z0-9]*[a-z0-9]))*"
	ClusterName string `json:"clusterName,omitempty"`

	// FeedStatus contains the brief feed status in child cluster.
	//
	// +optional
	FeedStatus `json:",inline"`
}

// FeedStatus defines the feed status.
type FeedStatus struct {
	// Available indicates whether the feed status is synced successfully to corresponding Description.
	//
	// +optional
	Available bool `json:"available,omitempty"`

	// ReplicaStatus indicates the replica status of workload-type feed, such as Deployment/StatefulSet/Job.
	//
	// +optional
	ReplicaStatus `json:"replicaStatus,omitempty"`
}

// ReplicaStatus represents brief information about feed replicas running in child cluster.
// This is used for workload-type feeds.
type ReplicaStatus struct {
	// The generation observed by the workload controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Total number of non-terminated pods targeted by this workload (their labels match the selector).
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Total number of non-terminated pods targeted by this workload that have the desired template spec.
	// +optional
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`

	// currentReplicas is the number of Pods created by the workload controller from the StatefulSet version
	// indicated by currentRevision.
	// +optional
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`

	// readyReplicas is the number of pods targeted by this workload with a Ready Condition.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// Total number of available pods (ready for at least minReadySeconds) targeted by this workload.
	// +optional
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`

	// Total number of unavailable pods targeted by this workload. This is the total number of
	// pods that are still required for the workload to have 100% available capacity. They may
	// either be pods that are running but not yet available or pods that still have not been created.
	// +optional
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`

	// The number of pending and running pods.
	// +optional
	Active int32 `json:"active,omitempty"`

	// The number of pods which reached phase Succeeded.
	// +optional
	Succeeded int32 `json:"succeeded,omitempty"`

	// The number of pods which reached phase Failed.
	// +optional
	Failed int32 `json:"failed,omitempty"`
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
	// +kubebuilder:validation:Minimum=0
	Weight int32 `json:"weight,omitempty"`
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
}

// DividingScheduling describes how to divide replicas into target clusters.
type DividingScheduling struct {
	// Type of dividing replica scheduling.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Static;Dynamic
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default=Static
	Type ReplicaDividingType `json:"type"`

	// DynamicDividing describes how to divide replicas into target clusters dynamically.
	//
	// +optional
	DynamicDividing *DynamicDividing `json:"dynamicDividing,omitempty"`
}

// DynamicDividing describes how to divide replicas into target clusters dynamically.
type DynamicDividing struct {
	// Type of dynamic dividing replica strategy.
	//
	// +required
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=Spread;Binpack
	// +kubebuilder:validation:Type=string
	// +kubebuilder:default=Spread
	Strategy DynamicDividingStrategy `json:"strategy"`

	// TopologySpreadConstraints describes how a group of replicas ought to spread across topology
	// domains. Scheduler will schedule pods in a way which abides by the constraints.
	// All topologySpreadConstraints are ANDed.
	// Present only for spread divided scheduling.
	//
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`

	// PreferredClusters describes the assigning preference. If we have a preference for cluster group A
	// compared to cluster group B (i.e., group A has a larger Weight), desired replicas will be assigned
	// to cluster group A as many as possible, while the rest ones will be assigned to cluster group B.
	//
	// +optional
	PreferredClusters []corev1.PreferredSchedulingTerm `json:"preferredClusters,omitempty"`

	// MinClusters describes the lower bound number of target clusters.
	//
	// +optional
	MinClusters *int32 `json:"minClusters,omitempty"`

	// MaxClusters describes the upper bound number of target clusters.
	//
	// +optional
	MaxClusters *int32 `json:"maxClusters,omitempty"`
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

	// DynamicReplicaDividingType divides replicas by cluster resource predictor.
	DynamicReplicaDividingType ReplicaDividingType = "Dynamic"
)

type DynamicDividingStrategy string

const (
	// SpreadDividingStrategy spreads out replicas as much as possible.
	SpreadDividingStrategy DynamicDividingStrategy = "Spread"

	// BinpackDividingStrategy aggregates replicas as much as possible.
	BinpackDividingStrategy DynamicDividingStrategy = "Binpack"
)

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SubscriptionList contains a list of Subscription
type SubscriptionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Subscription `json:"items"`
}
