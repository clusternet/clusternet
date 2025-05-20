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

package tainttoleration

import (
	"context"
	"fmt"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	v1helper "k8s.io/component-helpers/scheduling/corev1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/helper"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins/names"
)

// Name is the name of the plugin used in the plugin registry and configurations.
const Name = names.TaintToleration

// TaintToleration is a plugin that checks if a subscription tolerates a cluster's taints.
type TaintToleration struct {
	handle framework.Handle
}

var _ framework.FilterPlugin = &TaintToleration{}
var _ framework.ScorePlugin = &TaintToleration{}

// Name returns name of the plugin. It is used in logs, etc.
func (pl *TaintToleration) Name() string {
	return Name
}

// Filter invoked at the filter extension point.
func (pl *TaintToleration) Filter(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, cluster *clusterapi.ManagedCluster) *framework.Status {
	if cluster == nil {
		return framework.AsStatus(fmt.Errorf("invalid cluster"))
	}

	filterPredicate := func(t *v1.Taint) bool {
		// only interested in NoSchedule and NoExecute taints.
		return t.Effect == v1.TaintEffectNoSchedule || t.Effect == v1.TaintEffectNoExecute
	}

	taint, isUntolerated := v1helper.FindMatchingUntoleratedTaint(cluster.Spec.Taints, sub.Spec.ClusterTolerations, filterPredicate)
	if !isUntolerated {
		return nil
	}

	errReason := fmt.Sprintf("clusters(s) had taint {%s: %s}, that the subscription didn't tolerate",
		taint.Key, taint.Value)
	return framework.NewStatus(framework.UnschedulableAndUnresolvable, errReason)
}

// getAllTolerationEffectPreferNoSchedule gets the list of all Tolerations with Effect PreferNoSchedule or with no effect.
// copied from k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration/taint_toleration.go
func getAllTolerationPreferNoSchedule(tolerations []v1.Toleration) (tolerationList []v1.Toleration) {
	for _, toleration := range tolerations {
		// Empty effect means all effects which includes PreferNoSchedule, so we need to collect it as well.
		if len(toleration.Effect) == 0 || toleration.Effect == v1.TaintEffectPreferNoSchedule {
			tolerationList = append(tolerationList, toleration)
		}
	}
	return
}

// CountIntolerableTaintsPreferNoSchedule gives the count of intolerable taints of a subscription with effect PreferNoSchedule
// copied from k8s.io/kubernetes/pkg/scheduler/framework/plugins/tainttoleration/taint_toleration.go
func countIntolerableTaintsPreferNoSchedule(taints []v1.Taint, tolerations []v1.Toleration) (intolerableTaints int) {
	for _, taint := range taints {
		// check only on taints that have effect PreferNoSchedule
		if taint.Effect != v1.TaintEffectPreferNoSchedule {
			continue
		}

		if !v1helper.TolerationsTolerateTaint(tolerations, &taint) {
			intolerableTaints++
		}
	}
	return
}

// Score invoked at the Score extension point.
func (pl *TaintToleration) Score(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, namespacedCluster string) (int64, *framework.Status) {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(namespacedCluster)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("invalid resource key: %s", namespacedCluster))
	}

	cluster, err := pl.handle.SharedInformerFactory().Clusters().V1beta1().ManagedClusters().Lister().ManagedClusters(ns).Get(name)
	if err != nil {
		return 0, framework.AsStatus(fmt.Errorf("getting cluster %s: %v", namespacedCluster, err))
	}

	score := int64(countIntolerableTaintsPreferNoSchedule(cluster.Spec.Taints,
		getAllTolerationPreferNoSchedule(sub.Spec.ClusterTolerations)))
	return score, nil
}

// NormalizeScore invoked after scoring all clusters.
func (pl *TaintToleration) NormalizeScore(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, scores framework.ClusterScoreList) *framework.Status {
	return helper.DefaultNormalizeScore(framework.MaxClusterScore, true, scores)
}

// ScoreExtensions of the Score plugin.
func (pl *TaintToleration) ScoreExtensions() framework.ScoreExtensions {
	return pl
}

// New initializes a new plugin and returns it.
func New(_ runtime.Object, h framework.Handle) (framework.Plugin, error) {
	return &TaintToleration{handle: h}, nil
}
