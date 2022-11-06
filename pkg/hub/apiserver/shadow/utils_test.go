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
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/restmapper"
)

func TestNormalizeAPIGroupResources(t *testing.T) {
	tests := []struct {
		name             string
		apiGroupResource *restmapper.APIGroupResources
		want             []metav1.APIResource
	}{
		{
			name: "batch",
			apiGroupResource: &restmapper.APIGroupResources{
				Group: metav1.APIGroup{
					Name: "batch",
					Versions: []metav1.GroupVersionForDiscovery{
						{
							GroupVersion: "batch/v2alpha1",
							Version:      "v2alpha1",
						},
						{
							GroupVersion: "batch/v1",
							Version:      "v1",
						},
						{
							GroupVersion: "batch/v1beta1",
							Version:      "v1beta1",
						},
						{
							GroupVersion: "batch/v1alpha1",
							Version:      "v1alpha1",
						},
					},
					PreferredVersion: metav1.GroupVersionForDiscovery{
						GroupVersion: "batch/v1",
						Version:      "v1",
					},
				},
				VersionedResources: map[string][]metav1.APIResource{
					"v2alpha1": {
						metav1.APIResource{
							Name:               "xyz",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Xyz",
							Verbs:              metav1.Verbs{"foo", "bar", "far"},
							ShortNames:         nil,
							Categories:         []string{"all"},
							StorageVersionHash: "",
						},
					},
					"v1": {
						metav1.APIResource{
							Name:               "xyz",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Xyz",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         nil,
							Categories:         []string{"all"},
							StorageVersionHash: "",
						},
						metav1.APIResource{
							Name:               "jobs",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Job",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         nil,
							Categories:         []string{"all"},
							StorageVersionHash: "mudhfqk/qZY=",
						},
						metav1.APIResource{
							Name:               "jobs/status",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Job",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         nil,
							Categories:         nil,
							StorageVersionHash: "",
						},
					},
					"v1beta1": {
						metav1.APIResource{
							Name:               "cronjobs",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "CronJob",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         []string{"cj"},
							Categories:         []string{"all"},
							StorageVersionHash: "h/JlFAZkyyY=",
						},
						metav1.APIResource{
							Name:               "cronjobs/status",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "CronJob",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         nil,
							Categories:         nil,
							StorageVersionHash: "",
						},
						metav1.APIResource{
							Name:               "scenarios",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Scenario",
							Verbs:              metav1.Verbs{"foo", "bar", "far"},
							ShortNames:         []string{"sc"},
							Categories:         []string{"all"},
							StorageVersionHash: "",
						},
					},
					"v1alpha1": {
						metav1.APIResource{
							Name:               "scenarios",
							SingularName:       "",
							Namespaced:         true,
							Group:              "",
							Version:            "",
							Kind:               "Scenario",
							Verbs:              metav1.Verbs{"foo", "bar"},
							ShortNames:         []string{"sc"},
							Categories:         []string{"all"},
							StorageVersionHash: "",
						},
					},
				},
			},
			want: []metav1.APIResource{
				{
					Name:               "jobs",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1",
					Kind:               "Job",
					Verbs:              metav1.Verbs{"foo", "bar"},
					ShortNames:         nil,
					Categories:         []string{"all"},
					StorageVersionHash: "mudhfqk/qZY=",
				},
				{
					Name:               "jobs/status",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1",
					Kind:               "Job",
					Verbs:              metav1.Verbs{"foo", "bar"},
					ShortNames:         nil,
					Categories:         nil,
					StorageVersionHash: "",
				},
				{
					Name:               "xyz",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1",
					Kind:               "Xyz",
					Verbs:              metav1.Verbs{"foo", "bar"},
					ShortNames:         nil,
					Categories:         []string{"all"},
					StorageVersionHash: "",
				},
				{
					Name:               "cronjobs",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1beta1",
					Kind:               "CronJob",
					Verbs:              metav1.Verbs{"foo", "bar"},
					ShortNames:         []string{"cj"},
					Categories:         []string{"all"},
					StorageVersionHash: "h/JlFAZkyyY=",
				},
				{
					Name:               "cronjobs/status",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1beta1",
					Kind:               "CronJob",
					Verbs:              metav1.Verbs{"foo", "bar"},
					ShortNames:         nil,
					Categories:         nil,
					StorageVersionHash: "",
				},
				{
					Name:               "scenarios",
					SingularName:       "",
					Namespaced:         true,
					Group:              "batch",
					Version:            "v1beta1",
					Kind:               "Scenario",
					Verbs:              metav1.Verbs{"foo", "bar", "far"},
					ShortNames:         []string{"sc"},
					Categories:         []string{"all"},
					StorageVersionHash: "",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeAPIGroupResources(tt.apiGroupResource); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got: %v\n, want %v\n", got, tt.want)
			}
		})
	}
}
