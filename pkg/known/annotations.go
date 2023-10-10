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

package known

// annotations
const (
	// AutoUpdateAnnotation is the name of an annotation which prevents reconciliation if set to "false"
	AutoUpdateAnnotation = "clusternet.io/autoupdate"

	// FeedProtectionAnnotation passes detailed message on protecting current object as a feed
	FeedProtectionAnnotation = "apps.clusternet.io/feed-protection"

	// SkipValidatingAnnotation indicates no validations will be applied when dry-run on shadow resources.
	// This is useful when you want to create a CR, but you don't want to declare a CRD in the parent kube-apiserver.
	SkipValidatingAnnotation = "apps.clusternet.io/skip-validating"

	// ObjectOwnedByDescriptionAnnotation is the name of an annotation which contains the description of the object owned by
	ObjectOwnedByDescriptionAnnotation = "apps.clusternet.io/owned-by-description"

	// LastAppliedConfigAnnotation is the annotation used to store the previous
	// configuration of a resource for use in a three way diff by UpdateApplyAnnotation.
	LastAppliedConfigAnnotation = "clusternet.io/last-applied-configuration"

	// IsDefaultClusterInitAnnotation represents an annotation that marks a Base as the default cluster initialization
	// workloads.
	IsDefaultClusterInitAnnotation = "clusters.clusternet.io/is-default-cluster-init"

	// ClusterInitBaseAnnotation records the name of the Base to be used for initialization after joining.
	// This annotation is set on ManagedCluster objects.
	// It is mainly used for legacy clusters that have already joined.
	ClusterInitBaseAnnotation = "clusters.clusternet.io/cluster-init-base"

	// ClusterInitSkipAnnotation indicates no initialization operations will be applied to this cluster.
	ClusterInitSkipAnnotation = "clusters.clusternet.io/skip-cluster-init"
)
