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
	utilpointer "k8s.io/utils/pointer"

	"github.com/clusternet/clusternet/pkg/apis/scheduler"
)

// SetDefaultsSchedulerConfiguration sets the defaults for the scheduler configuration.
func SetDefaultsSchedulerConfiguration(obj *SchedulerConfiguration) {
	if len(obj.Profiles) == 0 {
		obj.Profiles = append(obj.Profiles, SchedulerProfile{})
	}
	// Only apply a default scheduler name when there is a single profile.
	// Validation will ensure that every profile has a non-empty unique name.
	if len(obj.Profiles) == 1 && obj.Profiles[0].SchedulerName == "" {
		obj.Profiles[0].SchedulerName = scheduler.DefaultSchedulerName
	}

	if obj.PercentageOfClustersToScore == nil {
		obj.PercentageOfClustersToScore = utilpointer.Int32(DefaultPercentageOfClustersToScore)
	}

	if obj.PercentageOfClustersToTolerate == nil {
		obj.PercentageOfClustersToTolerate = utilpointer.Int32(DefaultPercentageOfClustersToTolerate)
	}

	// Add the default set of plugins and apply the configuration.
	for i := range obj.Profiles {
		prof := &obj.Profiles[i]
		setDefaultsSchedulerProfile(prof)
	}
}

func setDefaultsSchedulerProfile(prof *SchedulerProfile) {
	// Set default plugins.
	prof.Plugins = mergePlugins(getDefaultPlugins(), prof.Plugins)

	//TODO: Set default plugin configs.
}
