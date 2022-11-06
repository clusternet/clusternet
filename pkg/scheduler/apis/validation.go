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
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

// ValidateSchedulerConfiguration ensures validation of the SchedulerConfiguration struct
func ValidateSchedulerConfiguration(cc *SchedulerConfiguration) utilerrors.Aggregate {
	var errs []error
	profilesPath := field.NewPath("profiles")
	if len(cc.Profiles) == 0 {
		errs = append(errs, field.Required(profilesPath, ""))
	} else {
		existingProfiles := make(map[string]int, len(cc.Profiles))
		for i := range cc.Profiles {
			profile := &cc.Profiles[i]
			path := profilesPath.Index(i)
			errs = append(errs, validateSchedulerProfile(path, cc.APIVersion, profile)...)
			if idx, ok := existingProfiles[profile.SchedulerName]; ok {
				errs = append(errs, field.Duplicate(path.Child("schedulerName"), profilesPath.Index(idx).Child("schedulerName")))
			}
			existingProfiles[profile.SchedulerName] = i
		}
	}

	return utilerrors.Flatten(utilerrors.NewAggregate(errs))
}

func validateSchedulerProfile(path *field.Path, apiVersion string, profile *SchedulerProfile) []error {
	var errs []error
	if len(profile.SchedulerName) == 0 {
		errs = append(errs, field.Required(path.Child("schedulerName"), ""))
	}
	errs = append(errs, validatePluginConfig(path, apiVersion, profile)...)
	return errs
}

func validatePluginConfig(path *field.Path, apiVersion string, profile *SchedulerProfile) []error {
	var errs []error
	//TODO add more plugin config validation

	return errs
}
