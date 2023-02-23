/*
Copyright 2023 The Clusternet Authors.

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
	"testing"

	"k8s.io/apimachinery/pkg/util/validation/field"
)

func Test_validateSchedulerProfile(t *testing.T) {
	type args struct {
		path       *field.Path
		apiVersion string
		profile    *SchedulerProfile
	}
	tests := []struct {
		name string
		args args
		want int
	}{
		{
			name: "test1",
			args: args{
				path:       field.NewPath("profile").Index(0),
				apiVersion: "kubescheduler.config.k8s.io/v1beta3",
				profile: &SchedulerProfile{
					SchedulerName: "test1",
					Plugins:       nil,
					PluginConfig:  make([]PluginConfig, 0),
				},
			},
			want: 0,
		},
		{
			name: "test2",
			args: args{
				path:       field.NewPath("profile").Index(0),
				apiVersion: "kubescheduler.config.k8s.io/v1beta3",
				profile: &SchedulerProfile{
					SchedulerName: "",
					Plugins:       nil,
					PluginConfig:  make([]PluginConfig, 0),
				},
			},
			want: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateSchedulerProfile(tt.args.path, tt.args.apiVersion, tt.args.profile); len(got) != tt.want {
				t.Errorf("validateSchedulerProfile() = %v, want %v", got, tt.want)
			}
		})
	}
}
