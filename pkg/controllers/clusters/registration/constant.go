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

const (
	// ClusterRegistrationURL denotes the url of parent cluster
	ClusterRegistrationURL = "cluster-reg-parent-url"

	// ClusterRegistrationToken is the token used to temporarily authenticate with parent cluster
	// while registering as a child cluster.
	ClusterRegistrationToken = "cluster-reg-token"

	// ClusterRegistrationKubeconfig specifies the path to a kubeconfig file for parent cluster
	ClusterRegistrationKubeconfig = "cluster-reg-parent-kubeconfig"

	// UnsafeParentCA flag instruct to skip parent cluster CA verification (for token-based cluster registration)
	UnsafeParentCA = "cluster-reg-unsafe-parent-ca"

	// DefaultParentKubeConfig is used to store the retrieved parent cluster kubeconfig
	DefaultParentKubeConfig = "/etc/clusternet/kubeconfig/parent-cluster.config"
)
