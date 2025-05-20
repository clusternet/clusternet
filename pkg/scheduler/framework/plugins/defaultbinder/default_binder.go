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

package defaultbinder

import (
	"context"
	"fmt"
	"sort"

	jsonpatch "github.com/evanphx/json-patch"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Name is the name of the plugin used in the plugin registry and configurations.
const Name = names.DefaultBinder

// DefaultBinder binds subscriptions to clusters using a clusternet client.
type DefaultBinder struct {
	handle framework.Handle
}

var _ framework.BindPlugin = &DefaultBinder{}

// New creates a DefaultBinder.
func New(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &DefaultBinder{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *DefaultBinder) Name() string {
	return Name
}

// Bind binds subscriptions to clusters using the clusternet client.
func (pl *DefaultBinder) Bind(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	klog.V(3).InfoS("Attempting to bind subscription to clusters",
		"subscription", klog.KObj(sub), "clusters", targetClusters.BindingClusters)

	// use an ordered list
	sort.Stable(targetClusters)

	subCopy := sub.DeepCopy()
	subCopy.Status.BindingClusters = targetClusters.BindingClusters
	subCopy.Status.Replicas = targetClusters.Replicas
	subCopy.Status.SpecHash = utils.HashSubscriptionSpec(&subCopy.Spec)
	subCopy.Status.DesiredReleases = len(targetClusters.BindingClusters)

	oldData, err := utils.Marshal(sub)
	if err != nil {
		return framework.AsStatus(fmt.Errorf("failed to marshal original subscription: %v", err))
	}
	newData, err := utils.Marshal(subCopy)
	if err != nil {
		return framework.AsStatus(fmt.Errorf("failed to marshal new subscription: %v", err))
	}
	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return framework.AsStatus(fmt.Errorf("failed to create a merge patch: %v", err))
	}

	_, err = pl.handle.ClientSet().AppsV1alpha1().Subscriptions(sub.Namespace).Patch(ctx, sub.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	if err != nil {
		return framework.AsStatus(err)
	}
	return nil
}
