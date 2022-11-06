/*
Copyright 2022 The Clusternet Authors.

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

package feedinventory

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/feedinventory/deployment"
	"github.com/clusternet/clusternet/pkg/controllers/apps/feedinventory/replicaset"
	"github.com/clusternet/clusternet/pkg/controllers/apps/feedinventory/statefulset"
)

// Registry is a collection of all available workload parser.
type Registry map[schema.GroupVersionKind]PluginFactory

// PluginFactory is an interface that must be implemented for each plugin.
type PluginFactory interface {
	// Parser parses the raw data to get the replicas, resource requirements, replica jsonpath, etc.
	Parser(rawData []byte) (*int32, appsapi.ReplicaRequirements, string, error)
	// Name returns name of the plugin. It is used in logs, etc.
	Name() string
	// Kind returns the resource kind.
	Kind() string
}

// NewInTreeRegistry builds the registry with all the in-tree plugins.
func NewInTreeRegistry() Registry {
	deployPlugin := deployment.NewPlugin()
	replicasetPlugin := replicaset.NewPlugin()
	statefulSetPlugin := statefulset.NewPlugin()

	return map[schema.GroupVersionKind]PluginFactory{
		// Deployment
		{Group: "apps", Version: "v1", Kind: deployPlugin.Kind()}:            deployPlugin,
		{Group: "apps", Version: "v1beta1", Kind: deployPlugin.Kind()}:       deployPlugin,
		{Group: "apps", Version: "v1beta2", Kind: deployPlugin.Kind()}:       deployPlugin,
		{Group: "extensions", Version: "v1beta1", Kind: deployPlugin.Kind()}: deployPlugin,

		// ReplicaSet
		{Group: "apps", Version: "v1", Kind: replicasetPlugin.Kind()}:            replicasetPlugin,
		{Group: "extensions", Version: "v1beta1", Kind: replicasetPlugin.Kind()}: replicasetPlugin,

		// StatefulSet
		{Group: "apps", Version: "v1", Kind: statefulSetPlugin.Kind()}:            statefulSetPlugin,
		{Group: "apps", Version: "v1beta1", Kind: statefulSetPlugin.Kind()}:       statefulSetPlugin,
		{Group: "apps", Version: "v1beta2", Kind: statefulSetPlugin.Kind()}:       statefulSetPlugin,
		{Group: "extensions", Version: "v1beta1", Kind: statefulSetPlugin.Kind()}: statefulSetPlugin,
	}
}
