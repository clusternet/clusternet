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

package apis

import (
	"k8s.io/apimachinery/pkg/util/sets"
)

// Plugins include multiple extension points. When specified, the list of plugins for
// a particular extension point are the only ones enabled. If an extension point is
// omitted from the config, then the default set of plugins is used for that extension point.
// Enabled plugins are called in the order specified here, after default plugins. If they need to
// be invoked before default plugins, default plugins must be disabled and re-enabled here in desired order.
type Plugins struct {
	// PreFilter is a list of plugins that should be invoked at "PreFilter" extension point of the predictor framework.
	PreFilter PluginSet

	// Filter is a list of plugins that should be invoked when filtering out clusters that cannot run the requirements.
	Filter PluginSet

	// PostFilter is a list of plugins that are invoked after filtering phase, no matter whether filtering succeeds or not.
	PostFilter PluginSet

	// PreCompute is a list of plugins that are invoked before Computing.
	PreCompute PluginSet

	// Compute is a list of plugins that should be invoked when compute max available replicas for nodes that have passed the filtering phase.
	Compute PluginSet

	// PreScore is a list of plugins that are invoked before scoring.
	PreScore PluginSet

	// Score is a list of plugins that should be invoked when ranking clusters that have passed the filtering phase.
	Score PluginSet

	// PreAggregate is a list of plugins that are invoked before aggregating.
	PreAggregate PluginSet

	// Assign is a list of plugins that should be invoked when aggregating replicas.
	Aggregate PluginSet
}

// PluginSet specifies enabled and disabled plugins for an extension point.
// If an array is empty, missing, or nil, default plugins at that extension point will be used.
type PluginSet struct {
	// Enabled specifies plugins that should be enabled in addition to default plugins.
	// These are called after default plugins and in the same order specified here.
	Enabled []Plugin
	// Disabled specifies default plugins that should be disabled.
	// When all default plugins need to be disabled, an array containing only one "*" should be provided.
	Disabled []Plugin
}

// Plugin specifies a plugin name and its weight when applicable. Weight is used only for Score plugins.
type Plugin struct {
	// Name defines the name of plugin
	Name string
	// Weight defines the weight of plugin, only used for Score plugins.
	Weight int32
}

// Names returns the list of enabled plugin names.
func (p *Plugins) Names() []string {
	if p == nil {
		return nil
	}
	extensions := []PluginSet{
		p.PreFilter,
		p.Filter,
		p.PostFilter,
		p.PreCompute,
		p.Compute,
		p.PreScore,
		p.Score,
		p.PreAggregate,
		p.Aggregate,
	}
	n := sets.NewString()
	for _, e := range extensions {
		for _, pg := range e.Enabled {
			n.Insert(pg.Name)
		}
	}
	return n.List()
}
