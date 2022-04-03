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
	// Setting on hub and agent side.
	SocketConnection featuregate.Feature = "SocketConnection"

	// AppPusher allows deploying applications directly from parent cluster.
	// In case of security concerns for a child cluster, this feature gate could be disabled on agent side.
	// When disabled, clusternet-agent (works in Dual or Pull mode) is responsible to deploy resources
	// to current self cluster.
	//
	// owner: @dixudx
	// alpha: v0.2.0
	// Setting on agent side.
	AppPusher featuregate.Feature = "AppPusher"

	// Deployer indicates whether clusternet-hub works for application managements.
	// The scheduling parts are handled by clusternet-scheduler.
	//
	// owner: @dixudx
	// alpha: v0.2.0
	// Setting on hub side.
	Deployer featuregate.Feature = "Deployer"

	// ShadowAPI provides an apiserver to shadow all the Kubernetes objects, including CRDs.
	//
	// owner: @dixudx
	// alpha: v0.3.0
	// Setting on hub side.
	ShadowAPI featuregate.Feature = "ShadowAPI"

	// FeedInUseProtection postpones deletion of an object that is being referred as a feed in Subscriptions.
	//
	// owner: @dixudx
	// alpha: v0.4.0
	// Setting on hub side.
	FeedInUseProtection featuregate.Feature = "FeedInUseProtection"

	// Recovery ensures the resources deployed by Clusternet exist persistently in a child cluster.
	// This helps rollback unexpected operations (like deleting, updating) that occurred solely inside a child cluster,
	// unless those are made explicitly from parent cluster.
	//
	// owner: @dixudx
	// alpha: v0.8.0
	// Setting on agent side.
	Recovery featuregate.Feature = "Recovery"

	// FeedInventory runs default in-tree registry to parse the schema of a resource, such as Deployment,
	// Statefulset, etc.
	// This feature gate should be closed when external FeedInventory controller is used.
	//
	// owner: @dixudx
	// alpha: v0.9.0
	// Setting on hub side.
	FeedInventory featuregate.Feature = "FeedInventory"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(defaultClusternetFeatureGates))
}

// defaultClusternetFeatureGates consists of all known Kubernetes-specific and clusternet feature keys.
// To add a new feature, define a key for it above and add it here. The features will be
// available throughout Clusternet binaries.
var defaultClusternetFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	SocketConnection:    {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	AppPusher:           {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	Deployer:            {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	ShadowAPI:           {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FeedInUseProtection: {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	Recovery:            {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
	FeedInventory:       {Default: false, PreRelease: featuregate.Alpha, LockToDefault: false},
}
