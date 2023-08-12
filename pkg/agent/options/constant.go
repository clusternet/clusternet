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

package options

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

	// ClusterRegistrationNamespace flag specifies the cluster registration namespace
	ClusterRegistrationNamespace = "cluster-reg-namespace"

	// ClusterRegistrationType flag specifies the cluster type
	ClusterRegistrationType = "cluster-reg-type"

	// ClusterSyncMode flag specifies the sync mode between parent cluster and child cluster
	ClusterSyncMode = "cluster-sync-mode"

	// ClusterLabels flag specifies the labels for the cluster
	ClusterLabels = "cluster-labels"

	// ClusterStatusReportFrequency flag specifies the child cluster status updating frequency
	ClusterStatusReportFrequency = "cluster-status-update-frequency"

	// ClusterStatusCollectFrequency flag specifies the cluster status collecting frequency
	ClusterStatusCollectFrequency = "cluster-status-collect-frequency"

	// UseMetricsServer flag specifies whether to collect metrics from metrics server running in the cluster
	UseMetricsServer = "use-metrics-server"

	// PredictorAddress flag specifies the address of external predictor
	PredictorAddress = "predictor-addr"

	// PredictorDirectAccess flag indicates whether the predictor can be accessed directly by clusternet-scheduler
	PredictorDirectAccess = "predictor-direct-access"

	// PredictorPort flag specifies the port on which to serve built-in predictor
	PredictorPort = "predictor-port"

	// LabelAggregateThreshold flag specifies the threshold of common node labels that will be aggregated
	// to ManagedCluster object in parent cluster.
	LabelAggregateThreshold = "labels-aggregate-threshold"

	// LabelAggregatePrefix flag specifies the label prefix to be aggregated
	LabelAggregatePrefix = "labels-aggregate-prefix"
)

// default values
const (
	ParentClusterSecretName = "parent-cluster"

	// RegistrationNamePrefix is a prefix name for cluster registration
	RegistrationNamePrefix = "clusternet-cluster"

	// ClusterNameMaxLength is the max length for clustername
	ClusterNameMaxLength = 60

	// ClusterNamespaceMaxLength is the max length for clusternamespace
	ClusterNamespaceMaxLength = 63

	nameFmt                              = "[a-z0-9]([-a-z0-9]*[a-z0-9])?([a-z0-9]([-a-z0-9]*[a-z0-9]))*"
	namespaceFmt                         = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"
	serviceAccountTokenFmt               = `[A-Za-z0-9-_]*\.[A-Za-z0-9-_]*\.[A-Za-z0-9-_]*`
	DefaultClusterStatusCollectFrequency = 20 * time.Second
	DefaultClusterStatusReportFrequency  = 3 * time.Minute
)
