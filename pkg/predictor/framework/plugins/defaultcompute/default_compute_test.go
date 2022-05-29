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

package defaultcompute

import (
	"context"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
)

const (
	ResourceUnitZero             int64 = 0
	ResourceUnitCPU              int64 = 1000
	ResourceUnitMem              int64 = 1024 * 1024 * 1024
	ResourceUnitPod              int64 = 1
	ResourceUnitEphemeralStorage int64 = 1024 * 1024 * 1024
)

func makeNode(node string, milliCPU, memory, pods, ephemeralStorage int64) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: node},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:              *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				corev1.ResourceMemory:           *resource.NewQuantity(memory, resource.BinarySI),
				corev1.ResourcePods:             *resource.NewQuantity(pods, resource.DecimalSI),
				corev1.ResourceEphemeralStorage: *resource.NewQuantity(ephemeralStorage, resource.BinarySI),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:              *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				corev1.ResourceMemory:           *resource.NewQuantity(memory, resource.BinarySI),
				corev1.ResourcePods:             *resource.NewQuantity(pods, resource.DecimalSI),
				corev1.ResourceEphemeralStorage: *resource.NewQuantity(ephemeralStorage, resource.BinarySI),
			},
		},
	}
}

func makePod(node string, milliCPU, memory, ephemeralStorage int64) *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{
				{
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:              *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
							corev1.ResourceMemory:           *resource.NewQuantity(memory, resource.DecimalSI),
							corev1.ResourceEphemeralStorage: *resource.NewQuantity(ephemeralStorage, resource.BinarySI),
						},
					},
				},
			},
		},
	}
}

func makeReplicaRequirements(milliCPU, memory, ephemeralStorage int64) *appsapi.ReplicaRequirements {
	return &appsapi.ReplicaRequirements{
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:              *resource.NewMilliQuantity(milliCPU, resource.DecimalSI),
				corev1.ResourceMemory:           *resource.NewQuantity(memory, resource.BinarySI),
				corev1.ResourceEphemeralStorage: *resource.NewQuantity(ephemeralStorage, resource.BinarySI),
			},
		},
	}
}

func TestDefaultComputer(t *testing.T) {
	tests := []struct {
		nodeInfo     *framework.NodeInfo
		requirements *appsapi.ReplicaRequirements
		want         int32
	}{
		{
			nodeInfo: framework.NewNodeInfo(
				makeNode("machine1", 8*ResourceUnitCPU, 16*ResourceUnitMem, 4*ResourceUnitPod, 16*ResourceUnitEphemeralStorage),
				[]*corev1.Pod{
					makePod("machine1", 1*ResourceUnitCPU, 4*ResourceUnitMem, 2*ResourceUnitEphemeralStorage),
					makePod("machine1", 1*ResourceUnitCPU, 4*ResourceUnitMem, 3*ResourceUnitEphemeralStorage),
				}),
			requirements: makeReplicaRequirements(1*ResourceUnitCPU, 1*ResourceUnitMem, ResourceUnitZero),
			want:         2,
		},
		{
			nodeInfo: framework.NewNodeInfo(
				makeNode("machine2", 8*ResourceUnitCPU, 16*ResourceUnitMem, 4*ResourceUnitPod, 16*ResourceUnitEphemeralStorage),
				[]*corev1.Pod{
					makePod("machine2", 1*ResourceUnitCPU, 4*ResourceUnitMem, 2*ResourceUnitEphemeralStorage),
					makePod("machine2", 6*ResourceUnitCPU, 4*ResourceUnitMem, 4*ResourceUnitEphemeralStorage),
				}),
			requirements: makeReplicaRequirements(1*ResourceUnitCPU, 1*ResourceUnitMem, ResourceUnitZero),
			want:         1,
		},
		{
			nodeInfo: framework.NewNodeInfo(
				makeNode("machine3", 8*ResourceUnitCPU, 16*ResourceUnitMem, 4*ResourceUnitPod, 16*ResourceUnitEphemeralStorage),
				[]*corev1.Pod{
					makePod("machine3", 1*ResourceUnitCPU, 4*ResourceUnitMem, 2*ResourceUnitEphemeralStorage),
					makePod("machine3", 1*ResourceUnitCPU, 11*ResourceUnitMem, 1*ResourceUnitEphemeralStorage),
				}),
			requirements: makeReplicaRequirements(1*ResourceUnitCPU, 1*ResourceUnitMem, ResourceUnitZero),
			want:         1,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			p, _ := New(nil, nil)
			maxAvailableReplicas, status := p.(framework.ComputePlugin).Compute(context.Background(), test.requirements, test.nodeInfo)
			if got := status.AsError(); got != nil {
				t.Errorf("status does not match %q, want nil", got)
			}
			if maxAvailableReplicas != test.want {
				t.Errorf("got different maxAvailableReplicas got %q, want %q",
					maxAvailableReplicas, test.want)
			}
		})
	}
}
