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

// rbac
const (
	// ManagedClusterRole is the default role for ManagedCluster objects
	ManagedClusterRole = "clusternet-managedcluster-role"
	// SocketsClusterRoleNamePrefix is the prefix name of Sockets clusterrole
	SocketsClusterRoleNamePrefix = "clusternet-"

	// MultiClusterServiceSyncerRole is the default role  to syncer mcs-related resources
	MultiClusterServiceSyncerRole = "mcs-syncer"
	// MultiClusterServiceNamespace is the default namespace to store mcs-related resources
	MultiClusterServiceNamespace = "clusternet-mcs"
)
