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

// This file defines the predictor framework plugin interfaces.
// The interfaces are copied from k8s.io/kubernetes/pkg/scheduler/framework/interface.go and modified

package interfaces

import (
	"context"
	"math"

	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	predictorapis "github.com/clusternet/clusternet/pkg/predictor/apis"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
)

// NodeScoreList declares a list of nodes and their scores.
type NodeScoreList []NodeScore

// NodeScore is a struct with node name and score.
type NodeScore struct {
	Name                 string
	Score                int64
	MaxAvailableReplicas int32
}

// PluginToNodeReplicas declares a map from plugin name to its NodeScoreList.
type PluginToNodeReplicas map[string]NodeScoreList

// PluginToNodeScores declares a map from plugin name to its NodeScoreList.
type PluginToNodeScores map[string]NodeScoreList

// NodeToStatusMap declares map from node name to its status.
type NodeToStatusMap map[string]*Status

// statusPrecedence defines a map from status to its precedence, larger value means higher precedent.
var statusPrecedence = map[Code]int{
	Error:         3,
	Unpredictable: 1,
	// Any other statuses we know today, `Skip`, will take precedence over `Success`.
	Success: -1,
}

const (
	// MaxNodeScore is the maximum score a Score plugin is expected to return.
	MaxNodeScore int64 = 100

	// MinNodeScore is the minimum score a Score plugin is expected to return.
	MinNodeScore int64 = 0

	// MaxTotalScore is the maximum total score.
	MaxTotalScore int64 = math.MaxInt64
)

// PluginToStatus maps plugin name to status. Currently, used to identify which Filter plugin
// returned which status.
type PluginToStatus map[string]*Status

// Merge merges the statuses in the map into one. The resulting status code have the following
// precedence: Error, Unpredictable.
func (p PluginToStatus) Merge() *Status {
	if len(p) == 0 {
		return nil
	}

	finalStatus := NewStatus(Success)
	for _, s := range p {
		if s.Code() == Error {
			finalStatus.err = s.AsError()
		}
		if statusPrecedence[s.Code()] > statusPrecedence[finalStatus.code] {
			finalStatus.code = s.Code()
			// Same as code, we keep the most relevant failedPlugin in the returned Status.
			finalStatus.failedPlugin = s.FailedPlugin()
		}

		for _, r := range s.reasons {
			finalStatus.AppendReason(r)
		}
	}

	return finalStatus
}

// Plugin is the parent type for all the predictor framework plugins.
type Plugin interface {
	Name() string
}

// PreFilterPlugin is an interface that must be implemented by "PreFilter" plugins.
// These plugins are called at the beginning of the predicting cycle.
type PreFilterPlugin interface {
	Plugin

	// PreFilter is called at the beginning of the predicting cycle. All PreFilter
	// plugins must return success or the requirements will be rejected.
	PreFilter(ctx context.Context, requirements *appsapi.ReplicaRequirements) *Status
}

// FilterPlugin is an interface for Filter plugins. These plugins are called at the
// filter extension point for filtering out hosts that cannot run a requirement.
// This concept used to be called 'predicate' in the original scheduler.
// These plugins should return "Success", "Unpredictable" or "Error" in Status.code.
// However, the predictor accepts other valid codes as well.
// Anything other than "Success" will lead to exclusion of the given host from
// running the requirements.
type FilterPlugin interface {
	Plugin

	// Filter is called by the predictor framework.
	// All FilterPlugins should return "Success" to declare that
	// the given node fits the requirements. If Filter doesn't return "Success",
	// it will return Unpredictable or Error.
	// For the node being evaluated, Filter plugins should look at the passed
	// nodeInfo 's information (e.g., requirement considered to be running on the node).
	Filter(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeInfo *NodeInfo) *Status
}

// PostFilterPlugin is an interface for "PostFilter" plugins. These plugins are called
// after requirements cannot be predicted.
type PostFilterPlugin interface {
	Plugin

	// PostFilter is called by the predictor framework.
	// A PostFilter plugin should return one of the following statuses:
	// - Unpredictable: the plugin gets executed successfully but the requirements cannot be made predictable.
	// - Success: the plugin gets executed successfully and the requirements can be made predictable.
	// - Error: the plugin aborts due to some internal error.
	PostFilter(ctx context.Context, requirements *appsapi.ReplicaRequirements, filteredNodeStatusMap NodeToStatusMap) (*PostFilterResult, *Status)
}

// PreComputePlugin is an interface for "PreCompute" plugin. PreCompute is an
// informational extension point. These are meant to prepare the state of
// the compute.
type PreComputePlugin interface {
	Plugin

	// PreCompute is called by the predictor framework after a list of managed nodes
	// passed the filtering phase. All pre-compute plugins must return success or
	// the requirements will be rejected.
	PreCompute(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodesInfo []*NodeInfo) *Status
}

// ComputePlugin is an interface that must be implemented by "Compute" plugins to
// compute acceptable replicas for each filtered node.
type ComputePlugin interface {
	Plugin

	// Compute is called on each filtered node. It will compute acceptable replicas
	// for each filtered node.
	Compute(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeInfo *NodeInfo) (int32, *Status)
}

// PreScorePlugin is an interface for "PreScore" plugin. PreScore is an
// informational extension point. Plugins will be called with a list of nodes
// that passed the filtering phase. A plugin may use this data to update internal
// state or to generate logs/metrics.
type PreScorePlugin interface {
	Plugin

	// PreScore is called by the predictor framework after a list of nodes
	// passed the filtering phase. All preScore plugins must return success or
	// the requirements will be rejected
	PreScore(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodes []*v1.Node) *Status
}

// ScoreExtensions is an interface for Score extended functionality.
type ScoreExtensions interface {
	// NormalizeScore is called for all node scores produced by the same plugin's "Score"
	// method. A successful run of NormalizeScore will update the scores list and return
	// a success status.
	NormalizeScore(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores NodeScoreList) *Status
}

// ScorePlugin is an interface that must be implemented by "Score" plugins to rank
// nodes that passed the filtering phase.
type ScorePlugin interface {
	Plugin

	// Score is called on each filtered node. It must return success and an integer
	// indicating the rank of the node. All scoring plugins must return success or
	// the requirements will be rejected.
	Score(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeName string) (int64, *Status)

	// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
	ScoreExtensions() ScoreExtensions
}

// PreAggregatePlugin is an interface for "PreAggregate" plugin. PreAggregate is an
// informational extension point. These are meant to prepare the state of
// the aggregate.
type PreAggregatePlugin interface {
	Plugin

	// PreAggregate is called by the predictor framework after a list of managed Nodes
	// passed the filtering phase. All pre-aggregate plugins must return success or
	// the requirements will be rejected.
	PreAggregate(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores NodeScoreList) *Status
}

// AggregatePlugin is an interface that must be implemented by "Aggregate" plugins to aggregate replicas
// for all selected nodes.
type AggregatePlugin interface {
	Plugin

	// Aggregate is called on all selected nodes. It will calculate the sum of acceptable
	// replicas for selected nodes. The return result is a map whose key indicates the
	// specific calculation strategy and value is max acceptable replicas.
	Aggregate(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores NodeScoreList) (schedulerapi.PredictorReplicas, *Status)
}

// Framework manages the set of plugins in use by the predictor framework.
// Configured plugins are called at specified points in an predictor context.
type Framework interface {
	Handle

	// RunPreFilterPlugins runs the set of configured PreFilter plugins. It returns
	// *Status and its code is set to non-success if any of the plugins returns
	// anything but Success. If a non-success status is returned, then the predicting
	// cycle is aborted.
	RunPreFilterPlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements) *Status

	// RunPostFilterPlugins runs the set of configured PostFilter plugins.
	// PostFilter plugins can either be informational, in which case should be configured
	// to execute first and return Unpredictable status, or ones that try to change the
	// cluster state to make the requirements potentially predictable in a future predicting cycle.
	RunPostFilterPlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, filteredNodeStatusMap NodeToStatusMap) (*PostFilterResult, *Status)

	// RunPreComputePlugins runs the set of configured PreCompute plugins. It returns
	// *Status and its code is set to non-success if any of the plugins returns
	// anything but Success. If a non-success status is returned, then the predicting
	// cycle is aborted.
	RunPreComputePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodesInfo []*NodeInfo) *Status

	// RunComputePlugins runs the set of configured Compute plugins. It returns one number that
	// stores for each Compute plugin. It also returns *Status, which is set to non-success
	// if any of the plugins returns a non-success status.
	RunComputePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodesInfo []*NodeInfo, availableList NodeScoreList) (NodeScoreList, *Status)

	// RunPreAggregatePlugins runs the set of configured PreAggregate plugins. It returns
	// *Status and its code is set to non-success if any of the plugins returns
	// anything but Success. If a non-success status is returned, then the predicting
	// cycle is aborted.
	RunPreAggregatePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores NodeScoreList) *Status

	// RunAggregatePlugins runs the set of configured Aggregate plugins.  It returns a map
	// whose key indicates the specific calculation strategy and value is max acceptable replicas
	// It also returns *Status, which is set to non-success if any of the plugins returns
	// a non-success status.
	RunAggregatePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores NodeScoreList) (schedulerapi.PredictorReplicas, *Status)

	// HasFilterPlugins returns true if at least one Filter plugin is defined.
	HasFilterPlugins() bool

	// HasPostFilterPlugins returns true if at least one PostFilter plugin is defined.
	HasPostFilterPlugins() bool

	// HasScorePlugins returns true if at least one Score plugin is defined.
	HasScorePlugins() bool

	// HasComputePlugins returns true if at least one Compute plugin is defined.
	HasComputePlugins() bool

	// HasAggregatePlugins returns true if at least one Aggregate plugin is defined.
	HasAggregatePlugins() bool

	// ListPlugins returns a map of extension point name to list of configured Plugins.
	ListPlugins() *predictorapis.Plugins

	// ProfileName returns the profile name associated to this framework.
	ProfileName() string
}

// Handle provides data and some tools that plugins can use. It is
// passed to the plugin factories at the time of plugin initialization. Plugins
// must store and use this handle to call framework functions.
type Handle interface {
	// PluginsRunner abstracts operations to run some plugins.
	PluginsRunner

	// ClientSet returns a clientSet.
	ClientSet() clientset.Interface

	// KubeConfig returns the raw kubeconfig.
	KubeConfig() *restclient.Config

	// EventRecorder returns an event recorder.
	EventRecorder() record.EventRecorder

	SharedInformerFactory() informers.SharedInformerFactory

	// Parallelizer returns a parallelizer holding parallelism for predictor.
	Parallelizer() parallelize.Parallelizer
}

// PostFilterResult wraps needed info for predictor framework to act upon PostFilter phase.
type PostFilterResult struct {
	NominatedNodeName string
}

// PluginsRunner abstracts operations to run some plugins.
// This is used by preemption PostFilter plugins when evaluating the feasibility of
// predicting the replicas of requirements on nodes when certain running pods get evicted.
type PluginsRunner interface {
	// RunPreScorePlugins runs the set of configured PreScore plugins. If any
	// of these plugins returns any status other than "Success", the given requirements is rejected.
	RunPreScorePlugins(context.Context, *appsapi.ReplicaRequirements, []*v1.Node) *Status

	// RunScorePlugins runs the set of configured Score plugins. It returns a map that
	// stores for each Score plugin name the corresponding NodeScoreList(s).
	// It also returns *Status, which is set to non-success if any of the plugins returns
	// a non-success status.
	RunScorePlugins(context.Context, *appsapi.ReplicaRequirements, []*v1.Node) (PluginToNodeScores, *Status)

	// RunFilterPlugins runs the set of configured Filter plugins for requirements on
	// the given node.
	RunFilterPlugins(context.Context, *appsapi.ReplicaRequirements, *NodeInfo) PluginToStatus
}
