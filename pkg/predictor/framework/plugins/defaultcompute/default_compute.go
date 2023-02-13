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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/predictor/framework/plugins/names"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Name is the name of the plugin used in the plugin registry and configurations.
const Name = names.DefaultComputer

// DefaultComputer compute replicas according to the resource.
type DefaultComputer struct {
	handle framework.Handle
}

var _ framework.ComputePlugin = &DefaultComputer{}

// New creates a DefaultAssigner.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &DefaultComputer{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *DefaultComputer) Name() string {
	return Name
}

// Compute compute acceptable replicas for each filtered node
func (pl *DefaultComputer) Compute(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeInfo *framework.NodeInfo) (int32, *framework.Status) {
	klog.V(5).InfoS("Attempting to compute replicas in nodes", "name :", nodeInfo.Node().Name)
	allocatableResource := utils.NewResource(nodeInfo.Node().Status.Allocatable)

	// occupied
	occupiedResource := utils.EmptyResource()
	occupiedPodNum := 0
	for _, pod := range nodeInfo.Pods {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		occupiedResource.AddPodRequest(&pod.Spec)
		occupiedPodNum++
	}
	occupiedResource.AddResourcePods(int64(occupiedPodNum))

	if err := allocatableResource.Sub(occupiedResource.ResourceList()); err != nil {
		return 0, framework.NewStatus(framework.Error, err.Error())
	}

	return int32(allocatableResource.MaxReplicaDivided(requirements.Resources.Requests)), nil
}
