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

import (
	"time"
)

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

	// ClusterRegistrationType flag specifies the cluster type
	ClusterRegistrationType = "cluster-reg-type"
)

// default values
const (
	SelfClusterLeaseName      = "self-cluster"
	SelfClusterLeaseNamespace = "edge-system"
	ParentClusterSecretName   = "parent-cluster"

	// RegistrationNamePrefix is a prefix name for cluster registration
	RegistrationNamePrefix = "clusternet-cluster-"

	// agent name
	RegisterAgentName = "clusternet-agent"
)

// labels
const (
	ClusterRegistrationLabel = "clusters.clusternet.io/registered-by"
	ClusterIDLabel           = "clusters.clusternet.io/cluster-id"
	ClusterNameLabel         = "clusters.clusternet.io/cluster-name"
)

// lease lock
const (
	DefaultLeaseDuration = 60 * time.Second
	DefaultRenewDeadline = 15 * time.Second
	DefaultRetryPeriod   = 5 * time.Second
)
