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
	// in clusternet-controller-manager
	ChildClusterSecretName = "child-cluster-deployer"

	// ClusterAPIServerURLKey denotes the apiserver address
	ClusterAPIServerURLKey = "apiserver-advertise-url"

	// ClusternetSystemNamespace is the default system namespace where we place system components.
	// This could be re-configured with flag "--leader-elect-resource-namespace"
	ClusternetSystemNamespace = "clusternet-system"

	// ClusternetReservedNamespace is the default namespace to store Manifest into
	ClusternetReservedNamespace = "clusternet-reserved"

	// ClusternetAppSA is the service account where we store credentials to deploy resources
	ClusternetAppSA = "clusternet-app-deployer"

	// ClusternetHubProxyServiceAccount is the service account that can be used for proxying requests to child clusters.
	// This will be also used by deployer in clusternet-controller-manager when flag "--anonymous-auth-supported" is
	// set to false.
	ClusternetHubProxyServiceAccount = "clusternet-hub-proxy"

	// nvidia gpu name
	NVIDIAGPUResourceName = "nvidia.com/gpu"
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

	// NoResyncPeriod indicates that informer resync should be delayed as long as possible
	NoResyncPeriod = 0 * time.Second

	// DefaultThreadiness defines default number of threads
	DefaultThreadiness = 10
)

// fields should be ignored when compared
const (
	MetaGeneration      = "/metadata/generation"
	CreationTimestamp   = "/metadata/creationTimestamp"
	ManagedFields       = "/metadata/managedFields"
	MetaUID             = "/metadata/uid"
	MetaSelflink        = "/metadata/selfLink"
	MetaResourceVersion = "/metadata/resourceVersion"

	SectionStatus = "/status"
)

const (
	Category = "clusternet.shadow"
)

const (
	// NoteLengthLimit denotes the maximum note length.
	// copied from k8s.io/kubernetes/pkg/apis/core/validation/events.go
	NoteLengthLimit = 1024
)

const (
	// DefaultRandomIDLength is the default length for random id
	DefaultRandomIDLength = 5
)

const (
	IndexKeyForSubscriptionUID = "subUid"
	IndexKeyForBaseUID         = "baseUid"
)
