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

package registration

// flags
const (
	// ClusterRegistrationURL flag denotes the url of parent cluster
	ClusterRegistrationURL = "cluster-reg-parent-url"

	// ClusterRegistrationToken flag is the token used to temporarily authenticate with parent cluster
	// while registering as a child cluster.
	ClusterRegistrationToken = "cluster-reg-token"

	// ClusterRegistrationUnsafeParentCA flag instruct to skip parent cluster CA verification (for token-based cluster registration)
	ClusterRegistrationUnsafeParentCA = "cluster-reg-unsafe-parent-ca"

	// ClusterRegistrationName flag specifies the cluster registration name
	ClusterRegistrationName = "cluster-reg-name"

	// ClusterRegistrationNamePrefix is a prefix for cluster registration name
	ClusterRegistrationNamePrefix = "cluster-reg-name-prefix"
)

// default values
const (
	SelfClusterLeaseName      = "self-cluster"
	SelfClusterLeaseNamespace = "edge-system"

	// RegistrationNamePrefix is a prefix name for cluster registration
	RegistrationNamePrefix = "clusternet-cluster-"
)

// labels
const (
	ClusterRegistrationLabel = "clusters.clusternet.io/registered-by"
	ClusterIDLabel           = "clusters.clusternet.io/cluster-id"
)
