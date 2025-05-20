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

package defaultassigner

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
)

// NameStaticAssigner is the name of the plugin used in the plugin registry and configurations.
const NameStaticAssigner = names.StaticAssigner

// StaticAssigner assigns replicas to clusters.
type StaticAssigner struct {
	handle framework.Handle
}

var _ framework.AssignPlugin = &StaticAssigner{}

// NewStaticAssigner creates a Default Assigner.
func NewStaticAssigner(_ runtime.Object, handle framework.Handle) (framework.Plugin, error) {
	return &StaticAssigner{handle: handle}, nil
}

// Name returns the name of the plugin.
func (pl *StaticAssigner) Name() string {
	return NameStaticAssigner
}

// Assign assigns subscriptions to clusters using the clusternet client.
func (pl *StaticAssigner) Assign(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) (framework.TargetClusters, *framework.Status) {
	klog.V(3).InfoS("Attempting to assign replicas to clusters",
		"subscription", klog.KObj(sub), "clusters", availableReplicas.BindingClusters)
	if sub.Spec.DividingScheduling == nil || sub.Spec.DividingScheduling.Type != appsapi.StaticReplicaDividingType {
		return framework.TargetClusters{}, framework.NewStatus(framework.Skip, "")
	}
	var err error
	var result framework.TargetClusters
	clusters := make([]*clusterapi.ManagedCluster, len(availableReplicas.BindingClusters))
	for i, name := range availableReplicas.BindingClusters {
		if clusters[i], err = pl.handle.ClusterCache().Get(name); err != nil {
			return framework.TargetClusters{}, framework.AsStatus(err)
		}
		result.BindingClusters = append(result.BindingClusters, name)
	}

	if err = StaticDivideReplicas(&result, sub, clusters, finv); err != nil {
		return framework.TargetClusters{}, framework.AsStatus(err)
	}

	return result, nil
}
