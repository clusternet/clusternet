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

package predictor

import (
	predictorapis "github.com/clusternet/clusternet/pkg/predictor/apis"
	"github.com/clusternet/clusternet/pkg/predictor/framework/plugins/names"
)

// getDefaultPlugins returns the default set of plugins.
func getDefaultPlugins() *predictorapis.Plugins {
	return &predictorapis.Plugins{
		PreFilter: predictorapis.PluginSet{},
		Filter: predictorapis.PluginSet{
			Enabled: []predictorapis.Plugin{
				{Name: names.TaintToleration},
			},
		},
		PostFilter: predictorapis.PluginSet{},
		PreCompute: predictorapis.PluginSet{},
		Compute: predictorapis.PluginSet{
			Enabled: []predictorapis.Plugin{
				{Name: names.DefaultComputer},
			},
		},
		PreScore: predictorapis.PluginSet{},
		Score: predictorapis.PluginSet{
			Enabled: []predictorapis.Plugin{
				{Name: names.TaintToleration, Weight: 3},
			},
		},
		PreAggregate: predictorapis.PluginSet{},
		Aggregate: predictorapis.PluginSet{
			Enabled: []predictorapis.Plugin{
				{Name: names.DefaultAggregator},
			},
		},
	}
}
