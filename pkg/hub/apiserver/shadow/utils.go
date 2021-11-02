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

package apiserver

import (
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/restmapper"
)

// normalizeAPIGroupResources returns resources with preferred version or highest semantic version
func normalizeAPIGroupResources(apiGroupResource *restmapper.APIGroupResources) []metav1.APIResource {
	var versionedResources []metav1.APIResource
	for version, vr := range apiGroupResource.VersionedResources {
		for _, resource := range vr {
			resource.Group = apiGroupResource.Group.Name
			resource.Version = version
			versionedResources = append(versionedResources, resource)
		}
	}

	// Ensure deterministic output.
	preferredVersion := apiGroupResource.Group.PreferredVersion.Version
	sort.SliceStable(versionedResources, func(i, j int) bool {
		if versionedResources[i].Version == versionedResources[j].Version {
			return versionedResources[i].Name < versionedResources[j].Name
		}

		// preferred version
		if versionedResources[i].Version == preferredVersion {
			return true
		}
		if versionedResources[j].Version == preferredVersion {
			return false
		}

		// compare kube-like version
		// Versions will be sorted based on GA/alpha/beta first and then major and minor versions.
		// e.g. v2, v1, v1beta2, v1beta1, v1alpha1.
		return version.CompareKubeAwareVersionStrings(versionedResources[i].Version, versionedResources[j].Version) > 0
	})

	// pick out preferred version or highest semantic version
	registered := make(map[string]bool)
	var normalizedVersionResources []metav1.APIResource
	for _, vr := range versionedResources {
		if registered[vr.Name] {
			continue
		}
		normalizedVersionResources = append(normalizedVersionResources, vr)
		registered[vr.Name] = true
	}
	return normalizedVersionResources
}
