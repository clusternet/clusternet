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

package scheduler

import (
	schedulerapis "github.com/clusternet/clusternet/pkg/scheduler/apis"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
)

// getDefaultPlugins returns the default set of plugins.
func getDefaultPlugins() *schedulerapis.Plugins {
	return &schedulerapis.Plugins{
		PreFilter: schedulerapis.PluginSet{},
		Filter: schedulerapis.PluginSet{
			Enabled: []schedulerapis.Plugin{
				{Name: names.TaintToleration},
			},
		},
		PostFilter:  schedulerapis.PluginSet{},
		PreEstimate: schedulerapis.PluginSet{},
		Estimate:    schedulerapis.PluginSet{},
		PreScore:    schedulerapis.PluginSet{},
		Score: schedulerapis.PluginSet{
			Enabled: []schedulerapis.Plugin{
				{Name: names.TaintToleration, Weight: 3},
			},
		},
		PreAssign: schedulerapis.PluginSet{},
		Assign: schedulerapis.PluginSet{
			Enabled: []schedulerapis.Plugin{
				{Name: names.StaticAssigner},
			},
		},
		Reserve: schedulerapis.PluginSet{},
		Permit:  schedulerapis.PluginSet{},
		PreBind: schedulerapis.PluginSet{},
		Bind: schedulerapis.PluginSet{
			Enabled: []schedulerapis.Plugin{
				{Name: names.DefaultBinder},
			},
		},
		PostBind: schedulerapis.PluginSet{},
	}
}
