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

package v1alpha1

import (
	"github.com/clusternet/clusternet/pkg/apis/scheduler"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
)

func pluginsNames(p *Plugins) []string {
	if p == nil {
		return nil
	}
	extensions := []PluginSet{
		p.PreFilter,
		p.Filter,
		p.PostFilter,
		p.PrePredict,
		p.Predict,
		p.Reserve,
		p.PreScore,
		p.Score,
		p.PreAssign,
		p.Assign,
		p.Permit,
		p.PreBind,
		p.Bind,
		p.PostBind,
	}
	n := sets.NewString()
	for _, e := range extensions {
		for _, pg := range e.Enabled {
			n.Insert(pg.Name)
		}
	}
	return n.List()
}

func SetDefaults_SchedulerConfiguration(obj *SchedulerConfiguration) {
	if len(obj.Profiles) == 0 {
		obj.Profiles = append(obj.Profiles, SchedulerProfile{})
	}
	// Only apply a default scheduler name when there is a single profile.
	// Validation will ensure that every profile has a non-empty unique name.
	if len(obj.Profiles) == 1 && obj.Profiles[0].SchedulerName == nil {
		obj.Profiles[0].SchedulerName = pointer.StringPtr(scheduler.DefaultSchedulerName)
	}

	// Add the default set of plugins and apply the configuration.
	for i := range obj.Profiles {
		prof := &obj.Profiles[i]
		setDefaults_SchedulerProfile(prof)
	}
}

func setDefaults_SchedulerProfile(prof *SchedulerProfile) {
	// Set default plugins.
	prof.Plugins = mergePlugins(getDefaultPlugins(), prof.Plugins)

	// Set default plugin configs.
	scheme := GetPluginArgConversionScheme()
	existingConfigs := sets.NewString()
	for j := range prof.PluginConfig {
		existingConfigs.Insert(prof.PluginConfig[j].Name)
		args := prof.PluginConfig[j].Args.Object
		if _, isUnknown := args.(*runtime.Unknown); isUnknown {
			continue
		}
		scheme.Default(args)
	}

	// Append default configs for plugins that didn't have one explicitly set.
	for _, name := range pluginsNames(prof.Plugins) {
		if existingConfigs.Has(name) {
			continue
		}
		gvk := SchemeGroupVersion.WithKind(name + "Args")
		args, err := scheme.New(gvk)
		if err != nil {
			// This plugin is out-of-tree or doesn't require configuration.
			continue
		}
		scheme.Default(args)
		args.GetObjectKind().SetGroupVersionKind(gvk)
		prof.PluginConfig = append(prof.PluginConfig, PluginConfig{
			Name: name,
			Args: runtime.RawExtension{Object: args},
		})
	}
}
