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

package utils

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func newQuantity(value string) resource.Quantity {
	quantity, err := resource.ParseQuantity(value)
	if err != nil {
		panic(err)
	}
	return quantity
}

func TestPluginParser(t *testing.T) {
	tests := []struct {
		name         string
		podSpec      corev1.PodSpec
		requirements appsapi.ReplicaRequirements
	}{
		{
			name: "empty requirements",
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "front-end",
						Image: "nginx",
					},
					{
						Name:  "rss-reader",
						Image: "rss-php-nginx:v1",
					},
				},
			},
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Limits:   map[corev1.ResourceName]resource.Quantity{},
					Requests: map[corev1.ResourceName]resource.Quantity{},
				},
			},
		},

		{
			name: "multiple-qos-with-foo-resource",
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "best-effort",
						Image: "nginx",
					},
					{
						Name:  "burstable-with-foo-resource",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("128Mi"),
								corev1.ResourceName("foo"): newQuantity("2"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("256Mi"),
								corev1.ResourceName("foo"): newQuantity("3"),
							},
						},
					},
					{
						Name:  "guaranteed",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
						},
					},
				},
			},
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("256Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("2"),
					},
					Limits: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("384Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("3"),
					},
				},
			},
		},

		{
			name: "init-containers-small-quota",
			podSpec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "init-container-1",
						Image: "image-test:tag1",
					},
					{
						Name:  "init-container-2",
						Image: "image-test:tag2",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("128Mi"),
								corev1.ResourceName("bar"): newQuantity("1"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("256Mi"),
								corev1.ResourceName("bar"): newQuantity("2"),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "best-effort",
						Image: "nginx",
					},
					{
						Name:  "burstable-with-foo-resource",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("128Mi"),
								corev1.ResourceName("foo"): newQuantity("2"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("256Mi"),
								corev1.ResourceName("foo"): newQuantity("3"),
							},
						},
					},
					{
						Name:  "guaranteed",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
						},
					},
				},
			},
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("256Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("2"),
						corev1.ResourceName("bar"): newQuantity("1"),
					},
					Limits: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("384Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("3"),
						corev1.ResourceName("bar"): newQuantity("2"),
					},
				},
			},
		},

		{
			name: "init-containers-large-quota",
			podSpec: corev1.PodSpec{
				InitContainers: []corev1.Container{
					{
						Name:  "init-container-1",
						Image: "image-test:tag1",
					},
					{
						Name:  "init-container-2",
						Image: "image-test:tag2",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("512Mi"),
								corev1.ResourceName("foo"): newQuantity("3"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("512Mi"),
								corev1.ResourceName("foo"): newQuantity("4"),
							},
						},
					},
				},
				Containers: []corev1.Container{
					{
						Name:  "best-effort",
						Image: "nginx",
					},
					{
						Name:  "burstable-with-foo-resource",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("128Mi"),
								corev1.ResourceName("foo"): newQuantity("2"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory:      newQuantity("256Mi"),
								corev1.ResourceName("foo"): newQuantity("3"),
							},
						},
					},
					{
						Name:  "guaranteed",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
						},
					},
				},
			},
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("512Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("3"),
					},
					Limits: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory:      newQuantity("512Mi"),
						corev1.ResourceCPU:         newQuantity("500m"),
						corev1.ResourceName("foo"): newQuantity("4"),
					},
				},
			},
		},

		{
			name: "multiple-qos-with-empty-request",
			podSpec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name:  "best-effort",
						Image: "nginx",
					},
					{
						Name:  "burstable",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("256Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
						},
					},
					{
						Name:  "guaranteed",
						Image: "nginx",
						Resources: corev1.ResourceRequirements{
							Limits: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceMemory: newQuantity("128Mi"),
								corev1.ResourceCPU:    newQuantity("500m"),
							},
						},
					},
				},
			},
			requirements: appsapi.ReplicaRequirements{
				Resources: corev1.ResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory: newQuantity("256Mi"),
						corev1.ResourceCPU:    newQuantity("1000m"),
					},
					Limits: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceMemory: newQuantity("384Mi"),
						corev1.ResourceCPU:    newQuantity("1000m"),
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requirements := GetReplicaRequirements(tt.podSpec)
			if !reflect.DeepEqual(requirements, tt.requirements) {
				t.Errorf("GetReplicaRequirements() requirements = %#v\n, requirements %#v", requirements, tt.requirements)
			}
		})
	}
}
