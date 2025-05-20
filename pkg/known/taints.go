/*
Copyright 2023 The Clusternet Authors.

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

const (
	// TaintClusterUnschedulable will be added when cluster becomes unschedulable
	// and removed when cluster becomes schedulable.
	TaintClusterUnschedulable = "clusters.clusternet.io/unschedulable"

	// TaintClusterInitialization will be added when cluster needs to be initialized after joining
	// and removed after initialization.
	TaintClusterInitialization = "clusters.clusternet.io/initialization"
)

const (
	// ClusterInitWaitingReason indicates the waiting reason for ClusterInit
	ClusterInitWaitingReason string = "ClusterInitWaiting"

	// ClusterInitDisabledReason indicates the disabled reason for ClusterInit
	ClusterInitDisabledReason string = "ClusterInitDisabled"

	// ClusterInitDoneReason indicates the disabled reason for ClusterInit
	ClusterInitDoneReason string = "ClusterInitDone"
)
