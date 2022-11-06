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
	"k8s.io/klog/v2"

	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
)

// getDefaultPlugins returns the default set of plugins.
func getDefaultPlugins() *Plugins {
	return &Plugins{
		PreFilter: PluginSet{},
		Filter: PluginSet{
			Enabled: []Plugin{
				{Name: names.TaintToleration},
			},
		},
		PostFilter: PluginSet{},
		PrePredict: PluginSet{},
		Predict: PluginSet{
			Enabled: []Plugin{
				{Name: names.Predictor},
			},
		},
		PreScore: PluginSet{},
		Score: PluginSet{
			Enabled: []Plugin{
				{Name: names.TaintToleration, Weight: 3},
			},
		},
		PreAssign: PluginSet{
			Enabled: []Plugin{
				{Name: names.DynamicAssigner},
			},
		},
		Assign: PluginSet{
			Enabled: []Plugin{
				{Name: names.StaticAssigner},
				{Name: names.DynamicAssigner},
			},
		},
		Reserve: PluginSet{},
		Permit:  PluginSet{},
		PreBind: PluginSet{},
		Bind: PluginSet{
			Enabled: []Plugin{
				{Name: names.DefaultBinder},
			},
		},
		PostBind: PluginSet{},
	}
}

// mergePlugins merges the custom set into the given default one, handling disabled sets.
func mergePlugins(defaultPlugins, customPlugins *Plugins) *Plugins {
	// Copied from "k8s.io/kubernetes/pkg/scheduler/apis/config/v1/default_plugins.go"

	if customPlugins == nil {
		return defaultPlugins
	}

	defaultPlugins.PreFilter = mergePluginSet(defaultPlugins.PreFilter, customPlugins.PreFilter)
	defaultPlugins.Filter = mergePluginSet(defaultPlugins.Filter, customPlugins.Filter)
	defaultPlugins.PostFilter = mergePluginSet(defaultPlugins.PostFilter, customPlugins.PostFilter)
	defaultPlugins.PrePredict = mergePluginSet(defaultPlugins.PrePredict, customPlugins.PrePredict)
	defaultPlugins.Predict = mergePluginSet(defaultPlugins.Predict, customPlugins.Predict)
	defaultPlugins.PreScore = mergePluginSet(defaultPlugins.PreScore, customPlugins.PreScore)
	defaultPlugins.Score = mergePluginSet(defaultPlugins.Score, customPlugins.Score)
	defaultPlugins.PreAssign = mergePluginSet(defaultPlugins.PreAssign, customPlugins.PreAssign)
	defaultPlugins.Assign = mergePluginSet(defaultPlugins.Assign, customPlugins.Assign)
	defaultPlugins.Reserve = mergePluginSet(defaultPlugins.Reserve, customPlugins.Reserve)
	defaultPlugins.Permit = mergePluginSet(defaultPlugins.Permit, customPlugins.Permit)
	defaultPlugins.PreBind = mergePluginSet(defaultPlugins.PreBind, customPlugins.PreBind)
	defaultPlugins.Bind = mergePluginSet(defaultPlugins.Bind, customPlugins.Bind)
	defaultPlugins.PostBind = mergePluginSet(defaultPlugins.PostBind, customPlugins.PostBind)
	return defaultPlugins
}

type pluginIndex struct {
	index  int
	plugin Plugin
}

func mergePluginSet(defaultPluginSet, customPluginSet PluginSet) PluginSet {
	// Copied from "k8s.io/kubernetes/pkg/scheduler/apis/config/v1/default_plugins.go"

	disabledPlugins := sets.NewString()
	enabledCustomPlugins := make(map[string]pluginIndex)
	// replacedPluginIndex is a set of index of plugins, which have replaced the default plugins.
	replacedPluginIndex := sets.NewInt()
	for _, disabledPlugin := range customPluginSet.Disabled {
		disabledPlugins.Insert(disabledPlugin.Name)
	}
	for index, enabledPlugin := range customPluginSet.Enabled {
		enabledCustomPlugins[enabledPlugin.Name] = pluginIndex{index, enabledPlugin}
	}
	var enabledPlugins []Plugin
	if !disabledPlugins.Has("*") {
		for _, defaultEnabledPlugin := range defaultPluginSet.Enabled {
			if disabledPlugins.Has(defaultEnabledPlugin.Name) {
				continue
			}
			// The default plugin is explicitly re-configured, update the default plugin accordingly.
			if customPlugin, ok := enabledCustomPlugins[defaultEnabledPlugin.Name]; ok {
				klog.InfoS("Default plugin is explicitly re-configured; overriding", "plugin", defaultEnabledPlugin.Name)
				// Update the default plugin in place to preserve order.
				defaultEnabledPlugin = customPlugin.plugin
				replacedPluginIndex.Insert(customPlugin.index)
			}
			enabledPlugins = append(enabledPlugins, defaultEnabledPlugin)
		}
	}

	// Append all the custom plugins which haven't replaced any default plugins.
	// Note: duplicated custom plugins will still be appended here.
	// If so, the instantiation of scheduler framework will detect it and abort.
	for index, plugin := range customPluginSet.Enabled {
		if !replacedPluginIndex.Has(index) {
			enabledPlugins = append(enabledPlugins, plugin)
		}
	}
	return PluginSet{Enabled: enabledPlugins}
}
