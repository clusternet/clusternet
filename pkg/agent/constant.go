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

package agent

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

	// ClusterRegistrationName flag specifies the cluster registration name
	ClusterRegistrationName = "cluster-reg-name"

	// ClusterRegistrationNamePrefix is a prefix for cluster registration name
	ClusterRegistrationNamePrefix = "cluster-reg-name-prefix"

	// ClusterRegistrationType flag specifies the cluster type
	ClusterRegistrationType = "cluster-reg-type"

	// ClusterSyncMode flag specifies the sync mode between parent cluster and child cluster
	ClusterSyncMode = "cluster-sync-mode"

	// ClusterStatusReportFrequency flag specifies the child cluster status updating frequency
	ClusterStatusReportFrequency = "cluster-status-update-frequency"

	// ClusterStatusCollectFrequency flag specifies the cluster status collecting frequency
	ClusterStatusCollectFrequency = "cluster-status-collect-frequency"
)

// default values
const (
	SelfClusterLeaseName      = "self-cluster"
	ClusternetSystemNamespace = "clusternet-system"
	ParentClusterSecretName   = "parent-cluster"
	ParentURLKey              = "parent-url"

	// RegistrationNamePrefix is a prefix name for cluster registration
	RegistrationNamePrefix = "clusternet-cluster"

	// max length for clustername
	ClusterNameMaxLength = 30
	// default length for random uid
	DefaultRandomUIDLength = 5

	nameFmt = "[a-z0-9]([-a-z0-9]*[a-z0-9])?([a-z0-9]([-a-z0-9]*[a-z0-9]))*"

	DefaultClusterStatusCollectFrequency = 20 * time.Second
	DefaultClusterStatusReportFrequency  = 3 * time.Minute
)

// lease lock
const (
	DefaultLeaseDuration = 60 * time.Second
	DefaultRenewDeadline = 15 * time.Second
	DefaultRetryPeriod   = 5 * time.Second
)
