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

import (
	"time"
)

const (
	// NamePrefixForClusternetObjects is a prefix name for generating Clusternet related objects for child cluster,
	// such as namespace, sa, etc
	NamePrefixForClusternetObjects = "clusternet-"

	// ChildClusterSecretName is the secret that stores credentials of child cluster, which will be used by deployer
	// in clusternet-hub
	ChildClusterSecretName = "child-cluster-deployer"

	// ClusterAPIServerURLKey denotes the apiserver address
	ClusterAPIServerURLKey = "apiserver-advertise-url"

	// ClusternetSystemNamespace is the system namespace where we place system components.
	ClusternetSystemNamespace = "clusternet-system"

	// ClusternetAppSA is the service account where we store credentials to deploy resources
	ClusternetAppSA = "clusternet-app-deployer"
)

// These are internal finalizer values to Clusternet, must be qualified name.
const (
	AppFinalizer            string = "apps.clusternet.io/finalizer"
	FeedProtectionFinalizer string = "apps.clusternet.io/feed-protection"
)

const (
	// DefaultResync means the default resync time
	DefaultResync = time.Hour * 12

	// DefaultRetryPeriod means the default retry period
	DefaultRetryPeriod = 5 * time.Second
)

const (
	Category = "clusternet.shadow"
)
