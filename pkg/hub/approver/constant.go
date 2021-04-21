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

package approver

import (
	"time"
)

// default values
const (
	// DefaultAPICallRetryInterval defines how long func should wait before retrying a failed API operation
	DefaultAPICallRetryInterval = 200 * time.Millisecond
)

// rbac
const (
	// ClusterRegistrationRole is the default clusterrole name for cluster registration requests
	ClusterRegistrationRole = "clusternet-cluster-registration-clusterrole"
	// ManagedClusterRole is the default role for ManagedCluster objects
	ManagedClusterRole = "clusternet-managedcluster-role"
)
