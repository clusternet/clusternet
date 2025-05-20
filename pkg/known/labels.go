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

// label key
const (
	NodeLabelsKeyPrefix       = "node.clusternet.io/"
	ClusterRegisteredByLabel  = "clusters.clusternet.io/registered-by"
	ClusterIDLabel            = "clusters.clusternet.io/cluster-id"
	ClusterNameLabel          = "clusters.clusternet.io/cluster-name"
	ClusterBootstrappingLabel = "clusters.clusternet.io/bootstrapping"

	ObjectCreatedByLabel = "clusternet.io/created-by"

	// the source info where this object belongs to or controlled by
	ConfigGroupLabel     = "apps.clusternet.io/config.group"
	ConfigVersionLabel   = "apps.clusternet.io/config.version"
	ConfigKindLabel      = "apps.clusternet.io/config.kind"
	ConfigNameLabel      = "apps.clusternet.io/config.name"
	ConfigNamespaceLabel = "apps.clusternet.io/config.namespace"
	ConfigUIDLabel       = "apps.clusternet.io/config.uid"

	ConfigSubscriptionUIDLabel       = "apps.clusternet.io/subs.uid"
	ConfigSubscriptionNameLabel      = "apps.clusternet.io/subs.name"
	ConfigSubscriptionNamespaceLabel = "apps.clusternet.io/subs.namespace"

	LabelServiceName      = "services.clusternet.io/multi-cluster-service-name"
	LabelServiceNameSpace = "services.clusternet.io/multi-cluster-service-namespace"
)

// label value
const (
	CredentialsAuto = "credentials-auto"
	RBACDefaults    = "rbac-defaults"

	ClusternetAgentName   = "clusternet-agent"
	ClusternetHubName     = "clusternet-hub"
	ClusternetCtrlMgrName = "clusternet-controller-manager"
)
