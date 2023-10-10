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

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// SocketConnection setups and serves a WebSocket connection.
	//
	// owner: @dixudx
	// alpha: v0.1.0
	// Setting on clusternet-hub and clusternet-agent side.
	// Also works on clusternet-controller-manager side to indicate whether to add rule "sockets/proxy" to
	// every dedicated ClusterRole object for child clusters.
	SocketConnection featuregate.Feature = "SocketConnection"

	// AppPusher allows deploying applications directly from parent cluster.
	// In case of security concerns for a child cluster, this feature gate could be disabled on agent side.
	// When disabled, clusternet-agent (works in Dual or Pull mode) is responsible to deploy resources
	// to current self cluster.
	//
	// owner: @dixudx
	// alpha: v0.2.0
	// Setting on clusternet-agent side.
	AppPusher featuregate.Feature = "AppPusher"

	// Deployer indicates whether clusternet-controller-manager works for application managements.
	// The scheduling parts are handled by clusternet-scheduler.
	//
	// owner: @dixudx
	// alpha: v0.2.0
	// Setting on clusternet-controller-manager side since v0.15.0.
	// (Setting on clusternet-hub side prior to v0.15.0.)
	Deployer featuregate.Feature = "Deployer"

	// ShadowAPI provides an apiserver to shadow all the Kubernetes objects, including CRDs.
	//
	// owner: @dixudx
	// alpha: v0.3.0
	// Setting on clusternet-hub side.
	ShadowAPI featuregate.Feature = "ShadowAPI"

	// FeedInUseProtection postpones deletion of an object that is being referred as a feed in Subscriptions.
	//
	// owner: @dixudx
	// alpha: v0.4.0
	// Setting on clusternet-controller-manager side since v0.15.0.
	// (Setting on clusternet-hub side prior to v0.15.0.)
	FeedInUseProtection featuregate.Feature = "FeedInUseProtection"

	// Recovery ensures the resources deployed by Clusternet exist persistently in a child cluster.
	// This helps rollback unexpected operations (like deleting, updating) that occurred solely inside a child cluster,
	// unless those are made explicitly from parent cluster.
	//
	// owner: @dixudx
	// alpha: v0.8.0
	// Setting on clusternet-agent side.
	Recovery featuregate.Feature = "Recovery"

	// FeedInventory runs default in-tree registry to parse the schema of a resource, such as Deployment,
	// Statefulset, etc.
	// This feature gate should be closed when external FeedInventory controller is used.
	//
	// owner: @dixudx
	// alpha: v0.9.0
	// Setting on clusternet-controller-manager side since v0.15.0.
	// (Setting on clusternet-hub side prior to v0.15.0.)
	FeedInventory featuregate.Feature = "FeedInventory"

	// Predictor will predictor child cluster resource before scheduling.
	// This feature gate needs a running predictor, either build-in or external.
	//
	// owner: @yinsenyan
	// alpha: v0.10.0
	// Setting on clusternet-agent side
	Predictor featuregate.Feature = "Predictor"

	// MultiClusterService indicates whether we allow service export and service import related controllers
	// to run. In some cases like integrating with submariner, this feature should be disabled.
	//
	// owner: @lmxia
	// alpha: v0.15.0
	// Setting on clusternet-controller-manager and clusternet-agent side.
	MultiClusterService featuregate.Feature = "MultiClusterService"

	// FailOver will migrate workloads from not-ready clusters to healthy spare clusters.
	//
	// owner: @dixudx
	// alpha: v0.16.0
	// Setting on clusternet-scheduler side.
	FailOver featuregate.Feature = "FailOver"

	// FeasibleClustersToleration indicates whether to tolerate failures on feasible clusters for dynamic scheduling
	// with predictors.
	// This helps improve scheduler's performance on dynamic scheduling with predictors.
	//
	// owner: @dixudx
	// alpha: v0.16.0
	// Setting on clusternet-scheduler side.
	FeasibleClustersToleration featuregate.Feature = "FeasibleClustersToleration"

	// ClusterInit helps initializing the cluster after joining.
	// All the initializing jobs are defined by a default Subscription, which
	// When a new ManagedCluster (with taint "clusters.clusternet.io/initialization:NoSchedule") joins,
	// a default Subscription will be performed to do some
	// TODO
	//
	// owner: @dixudx
	// alpha: v0.17.0
	// Setting on clusternet-controller-manager side.
	ClusterInit featuregate.Feature = "ClusterInit"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultClusternetFeatureGates))
}

// defaultClusternetFeatureGates consists of all known Kubernetes-specific and clusternet feature keys.
// To add a new feature, define a key for it above and add it here. The features will be
// available throughout Clusternet binaries.
var defaultClusternetFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	SocketConnection:           {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	AppPusher:                  {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	Deployer:                   {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	ShadowAPI:                  {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FeedInUseProtection:        {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	Recovery:                   {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FeedInventory:              {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	Predictor:                  {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	MultiClusterService:        {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FailOver:                   {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FeasibleClustersToleration: {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	ClusterInit:                {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
}
