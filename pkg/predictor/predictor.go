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

package predictor

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
	frameworkruntime "github.com/clusternet/clusternet/pkg/predictor/framework/runtime"
	"github.com/clusternet/clusternet/pkg/predictor/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
)

// findNodesThatFitRequirements filters the nodes to find the ones that fit the requirements based on the framework filter plugins.
func findNodesThatFitRequirements(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo,
) ([]*framework.NodeInfo, error) {
	// Run "prefilter" plugins.
	s := fwk.RunPreFilterPlugins(ctx, requirements)
	if !s.IsSuccess() {
		if !s.IsUnpredictable() {
			return nil, s.AsError()
		}
		return nil, s.AsError()
	}

	feasibleNodes, err := findNodesThatPassFilters(ctx, fwk, requirements, nodes)
	if err != nil {
		return nil, err
	}

	if klog.V(6).Enabled() {
		klog.V(6).InfoS("get feasibleNodes", "num", len(feasibleNodes))
	}

	return feasibleNodes, nil
}

// findNodesThatPassFilters finds the nodes that fit the filter plugins.
func findNodesThatPassFilters(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo,
) ([]*framework.NodeInfo, error) {

	// Create feasible list with enough space to avoid growing it
	// and allow assigning.
	feasibleNodes := make([]*framework.NodeInfo, len(nodes))

	if !fwk.HasFilterPlugins() {
		return nodes, nil
	}

	errCh := parallelize.NewErrorChannel()
	var feasibleNodesLen int32
	ctx, cancel := context.WithCancel(ctx)
	checkNode := func(i int) {
		nodeInfo := nodes[i]
		status := fwk.RunFilterPlugins(ctx, requirements, nodeInfo).Merge()
		if status.Code() == framework.Error {
			errCh.SendErrorWithCancel(status.AsError(), cancel)
			return
		}
		if status.IsSuccess() {
			length := atomic.AddInt32(&feasibleNodesLen, 1)
			feasibleNodes[length-1] = nodeInfo
		}
	}

	beginCheckNode := time.Now()
	statusCode := framework.Success
	defer func() {
		// We record Filter extension point latency here instead of in framework.go because framework.RunFilterPlugins
		// function is called for each node, whereas we want to have an overall latency for all nodes. .
		metrics.FrameworkExtensionPointDuration.WithLabelValues(frameworkruntime.Filter, statusCode.String(), fwk.ProfileName()).Observe(metrics.SinceInSeconds(beginCheckNode))
	}()

	// Stops searching for more nodes once the configured number of feasible nodes
	// are found.
	fwk.Parallelizer().Until(ctx, len(nodes), checkNode)

	feasibleNodes = feasibleNodes[:feasibleNodesLen]
	if err := errCh.ReceiveError(); err != nil {
		statusCode = framework.Error
		return nil, err
	}
	return feasibleNodes, nil
}

// computeReplicas calculate max accept replicas in each node  based on the framework calculate plugins.
func computeReplicas(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodes []*framework.NodeInfo,
) (framework.NodeScoreList, error) {
	availableList := make(framework.NodeScoreList, len(nodes))
	for i := range nodes {
		availableList[i] = framework.NodeScore{
			Name: klog.KObj(nodes[i].Node()).String(),
		}
	}

	// Initialize.
	for i := range availableList {
		availableList[i].MaxAvailableReplicas = -1
	}

	if !fwk.HasComputePlugins() {
		return nil, fmt.Errorf("no compute plugin is registry")
	}

	// Run PreCompute plugins.
	preComputeStatus := fwk.RunPreComputePlugins(ctx, requirements, nodes)
	if !preComputeStatus.IsSuccess() {
		return nil, preComputeStatus.AsError()
	}

	// Run the Compute plugins.
	nodeScoreList, compute := fwk.RunComputePlugins(ctx, requirements, nodes, availableList)
	if !compute.IsSuccess() {
		return nil, compute.AsError()
	}

	if klog.V(6).Enabled() {
		for i := range availableList {
			klog.V(6).InfoS("compute node's max available replicas", "nodes", nodeScoreList[i].Name, "max available replicas", availableList[i].MaxAvailableReplicas)
		}
	}
	return nodeScoreList, nil
}

// prioritizeNodes prioritizes the nodes by running the score plugins,
// which return a score for each node from the call to RunScorePlugins().
// The scores from each plugin are added together to make the score for that node, then
// any extenders are run as well.
// All scores are finally combined (added) to get the total weighted scores of all nodes
func prioritizeNodes(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	nodeInfos []*framework.NodeInfo,
	result framework.NodeScoreList,
) (framework.NodeScoreList, error) {
	// If no priority configs are provided, then all nodes will have a score of one.
	// This is required to generate the priority list in the required format
	if !fwk.HasScorePlugins() {
		for i := range result {
			result[i].Score = 1
		}
		return result, nil
	}

	nodes := make([]*corev1.Node, len(nodeInfos))
	for i := range nodeInfos {
		nodes[i] = nodeInfos[i].Node()
	}

	// Run PreScore plugins.
	preScoreStatus := fwk.RunPreScorePlugins(ctx, requirements, nodes)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	// Run the Score plugins.
	scoresMap, scoreStatus := fwk.RunScorePlugins(ctx, requirements, nodes)
	if !scoreStatus.IsSuccess() {
		return nil, scoreStatus.AsError()
	}

	if klog.V(6).Enabled() {
		for plugin, nodeScoreList := range scoresMap {
			for _, nodeScore := range nodeScoreList {
				klog.V(6).InfoS("Plugin scored node", "plugin", plugin, "node", nodeScore.Name, "score", nodeScore.Score)
			}
		}
	}

	// Summarize all scores.

	for i := range nodes {
		for j := range scoresMap {
			result[i].Score += scoresMap[j][i].Score
		}
	}

	if klog.V(6).Enabled() {
		for i := range result {
			klog.V(6).InfoS("Calculated node's final score", "node", result[i].Name, "score", result[i].Score)
		}
	}
	return result, nil
}

// aggregateReplicas aggregate the  max accept replicas in all selected nodes based on the framework aggregate plugins.
func aggregateReplicas(
	ctx context.Context,
	fwk framework.Framework,
	requirements *appsapi.ReplicaRequirements,
	result framework.NodeScoreList,
) (schedulerapi.PredictorReplicas, error) {
	if !fwk.HasAggregatePlugins() {
		return nil, fmt.Errorf("no calculate plugin is registry")
	}

	// Run PreAggregate plugins.
	preAggregateStatus := fwk.RunPreAggregatePlugins(ctx, requirements, result)
	if !preAggregateStatus.IsSuccess() {
		return nil, preAggregateStatus.AsError()
	}

	// Run the Aggregate plugins.
	acceptableReplicas, aggregateStatus := fwk.RunAggregatePlugins(ctx, requirements, result)
	if !aggregateStatus.IsSuccess() {
		return nil, aggregateStatus.AsError()
	}

	return acceptableReplicas, nil
}
