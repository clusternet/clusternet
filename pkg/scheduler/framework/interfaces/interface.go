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

// This file defines the scheduling framework plugin interfaces.
// The interfaces are copied from k8s.io/kubernetes/pkg/scheduler/framework/interface.go and modified

package interfaces

import (
	"context"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/types"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	schedulerapis "github.com/clusternet/clusternet/pkg/scheduler/apis"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
)

// ClusterScoreList declares a list of clusters and their scores.
type ClusterScoreList []ClusterScore

// ClusterScore is a struct with cluster id and score.
type ClusterScore struct {
	NamespacedName string // the namespaced name key of a ManagedCluster
	Score          int64
}

// PluginToClusterScores declares a map from plugin name to its ClusterScoreList.
type PluginToClusterScores map[string]ClusterScoreList

// ClusterToStatusMap declares map from cluster namespaced key to its status.
type ClusterToStatusMap map[string]*Status

// statusPrecedence defines a map from status to its precedence, larger value means higher precedent.
var statusPrecedence = map[Code]int{
	Error:                        3,
	UnschedulableAndUnresolvable: 2,
	Unschedulable:                1,
	// Any other statuses we know today, `Skip` or `Wait`, will take precedence over `Success`.
	Success: -1,
}

const (
	// MaxClusterScore is the maximum score a Score plugin is expected to return.
	MaxClusterScore int64 = 100

	// MinClusterScore is the minimum score a Score plugin is expected to return.
	MinClusterScore int64 = 0

	// MaxTotalScore is the maximum total score.
	MaxTotalScore int64 = math.MaxInt64
)

// PluginToStatus maps plugin name to status. Currently, used to identify which Filter plugin
// returned which status.
type PluginToStatus map[string]*Status

// Merge merges the statuses in the map into one. The resulting status code have the following
// precedence: Error, UnschedulableAndUnresolvable, Unschedulable.
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

// WaitingSubscription represents a subscription currently waiting in the permit phase.
type WaitingSubscription interface {
	// GetSubscription returns a reference to the waiting subscription.
	GetSubscription() *appsapi.Subscription

	// GetPendingPlugins returns a list of pending Permit plugin's name.
	GetPendingPlugins() []string

	// Allow declares the waiting subscription is allowed to be scheduled by the plugin named as "pluginName".
	// If this is the last remaining plugin to allow, then a success signal is delivered
	// to unblock the subscription.
	Allow(pluginName string)

	// Reject declares the waiting subscription Unschedulable.
	Reject(pluginName, msg string)
}

// Plugin is the parent type for all the scheduling framework plugins.
type Plugin interface {
	Name() string
}

// PreFilterPlugin is an interface that must be implemented by "PreFilter" plugins.
// These plugins are called at the beginning of the scheduling cycle.
type PreFilterPlugin interface {
	Plugin

	// PreFilter is called at the beginning of the scheduling cycle. All PreFilter
	// plugins must return success or the subscription will be rejected.
	PreFilter(ctx context.Context, sub *appsapi.Subscription) *Status
}

// FilterPlugin is an interface for Filter plugins. These plugins are called at the
// filter extension point for filtering out hosts that cannot run a subscription.
// This concept used to be called 'predicate' in the original scheduler.
// These plugins should return Success, Unschedulable or Error in Status.code.
// However, the scheduler accepts other valid codes as well.
// Anything other than "Success" will lead to exclusion of the given host from
// running the subscription.
type FilterPlugin interface {
	Plugin

	// Filter is called by the scheduling framework.
	// All FilterPlugins should return "Success" to declare that
	// the given managed cluster fits the subscription. If Filter doesn't return "Success",
	// it will return Unschedulable, UnschedulableAndUnresolvable or Error.
	// For the cluster being evaluated, Filter plugins should look at the passed
	// cluster's information (e.g., subscriptions considered to be running on the cluster).
	Filter(ctx context.Context, sub *appsapi.Subscription, cluster *clusterapi.ManagedCluster) *Status
}

// PostFilterPlugin is an interface for "PostFilter" plugins. These plugins are called
// after a subscription cannot be scheduled.
type PostFilterPlugin interface {
	Plugin

	// PostFilter is called by the scheduling framework.
	// A PostFilter plugin should return one of the following statuses:
	// - Unschedulable: the plugin gets executed successfully but the subscription cannot be made schedulable.
	// - Success: the plugin gets executed successfully and the subscription can be made schedulable.
	// - Error: the plugin aborts due to some internal error.
	//
	// Informational plugins should be configured ahead of other ones, and always return Unschedulable status.
	// Optionally, a non-nil PostFilterResult may be returned along with a Success status. For example,
	// a preemption plugin may choose to return nominatedClusterName, so that framework can reuse that to update the
	// preemptor subscription's .spec.status.nominatedClusterName field.
	PostFilter(ctx context.Context, sub *appsapi.Subscription, filteredClusterStatusMap ClusterToStatusMap) (*PostFilterResult, *Status)
}

// PreScorePlugin is an interface for "PreScore" plugin. PreScore is an
// informational extension point. Plugins will be called with a list of clusters
// that passed the filtering phase. A plugin may use this data to update internal
// state or to generate logs/metrics.
type PreScorePlugin interface {
	Plugin

	// PreScore is called by the scheduling framework after a list of managed clusters
	// passed the filtering phase. All prescore plugins must return success or
	// the subscription will be rejected
	PreScore(ctx context.Context, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster) *Status
}

// ScoreExtensions is an interface for Score extended functionality.
type ScoreExtensions interface {
	// NormalizeScore is called for all cluster scores produced by the same plugin's "Score"
	// method. A successful run of NormalizeScore will update the scores list and return
	// a success status.
	NormalizeScore(ctx context.Context, scores ClusterScoreList) *Status
}

// ScorePlugin is an interface that must be implemented by "Score" plugins to rank
// clusters that passed the filtering phase.
type ScorePlugin interface {
	Plugin

	// Score is called on each filtered cluster. It must return success and an integer
	// indicating the rank of the cluster. All scoring plugins must return success or
	// the subscription will be rejected.
	Score(ctx context.Context, sub *appsapi.Subscription, namespacedCluster string) (int64, *Status)

	// ScoreExtensions returns a ScoreExtensions interface if it implements one, or nil if not.
	ScoreExtensions() ScoreExtensions
}

// ReservePlugin is an interface for plugins with Reserve and Unreserve
// methods. These are meant to update the state of the plugin. This concept
// used to be called 'assume' in the original scheduler. These plugins should
// return only Success or Error in Status.code. However, the scheduler accepts
// other valid codes as well. Anything other than Success will lead to
// rejection of the subscription.
type ReservePlugin interface {
	Plugin

	// Reserve is called by the scheduling framework when the scheduler cache is
	// updated. If this method returns a failed Status, the scheduler will call
	// the Unreserve method for all enabled ReservePlugins.
	Reserve(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status
	// Unreserve is called by the scheduling framework when a reserved subscription was
	// rejected, an error occurred during reservation of subsequent plugins, or
	// in a later phase. The Unreserve method implementation must be idempotent
	// and may be called by the scheduler even if the corresponding Reserve
	// method for the same plugin was not called.
	Unreserve(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string)
}

// PreBindPlugin is an interface that must be implemented by "PreBind" plugins.
// These plugins are called before a subscription being scheduled.
type PreBindPlugin interface {
	Plugin

	// PreBind is called before binding a subscription. All prebind plugins must return
	// success or the subscription will be rejected and won't be sent for binding.
	PreBind(ctx context.Context, sub *appsapi.Subscription, clusterNamespaces []string) *Status
}

// PostBindPlugin is an interface that must be implemented by "PostBind" plugins.
// These plugins are called after a subscription is successfully bound to a group of ManagedCluster.
type PostBindPlugin interface {
	Plugin

	// PostBind is called after a subscription is successfully bound. These plugins are
	// informational. A common application of this extension point is for cleaning
	// up. If a plugin needs to clean-up its state after a subscription is scheduled and
	// bound, PostBind is the extension point that it should register.
	PostBind(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string)
}

// PermitPlugin is an interface that must be implemented by "Permit" plugins.
// These plugins are called before a subscription is bound to a group of ManagedCluster.
type PermitPlugin interface {
	Plugin

	// Permit is called before binding a subscription (and before prebind plugins). Permit
	// plugins are used to prevent or delay the binding of a Subscription. A permit plugin
	// must return success or wait with timeout duration, or the subscription will be rejected.
	// The subscription will also be rejected if the wait timeout or the subscription is rejected while
	// waiting. Note that if the plugin returns "wait", the framework will wait only
	// after running the remaining plugins given that no other plugin rejects the subscription.
	Permit(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) (*Status, time.Duration)
}

// BindPlugin is an interface that must be implemented by "Bind" plugins. Bind
// plugins are used to bind a subscription to a group of ManagedCluster.
type BindPlugin interface {
	Plugin

	// Bind plugins will not be called until all pre-bind plugins have completed. Each
	// bind plugin is called in the configured order. A bind plugin may choose whether
	// or not to handle the given Subscription. If a bind plugin chooses to handle a Subscription, the
	// remaining bind plugins are skipped. When a bind plugin does not handle a Subscription,
	// it must return Skip in its Status code. If a bind plugin returns an Error, the
	// subscription is rejected and will not be bound.
	Bind(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status
}

// Framework manages the set of plugins in use by the scheduling framework.
// Configured plugins are called at specified points in a scheduling context.
type Framework interface {
	Handle

	// RunPreFilterPlugins runs the set of configured PreFilter plugins. It returns
	// *Status and its code is set to non-success if any of the plugins returns
	// anything but Success. If a non-success status is returned, then the scheduling
	// cycle is aborted.
	RunPreFilterPlugins(ctx context.Context, sub *appsapi.Subscription) *Status

	// RunPostFilterPlugins runs the set of configured PostFilter plugins.
	// PostFilter plugins can either be informational, in which case should be configured
	// to execute first and return Unschedulable status, or ones that try to change the
	// cluster state to make the subscription potentially schedulable in a future scheduling cycle.
	RunPostFilterPlugins(ctx context.Context, sub *appsapi.Subscription, filteredClusterStatusMap ClusterToStatusMap) (*PostFilterResult, *Status)

	// RunReservePluginsReserve runs the Reserve method of the set of
	// configured Reserve plugins. If any of these calls returns an error, it
	// does not continue running the remaining ones and returns the error. In
	// such case, subscription will not be scheduled.
	RunReservePluginsReserve(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status

	// RunReservePluginsUnreserve runs the Unreserve method of the set of
	// configured Reserve plugins.
	RunReservePluginsUnreserve(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string)

	// RunPermitPlugins runs the set of configured Permit plugins. If any of these
	// plugins returns a status other than "Success" or "Wait", it does not continue
	// running the remaining plugins and returns an error. Otherwise, if any of the
	// plugins returns "Wait", then this function will create and add waiting subscription
	// to a map of currently waiting subscriptions and return status with "Wait" code.
	// Subscription will remain waiting subscription for the minimum duration returned by the Permit plugins.
	RunPermitPlugins(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status

	// WaitOnPermit will block, if the subscription is a waiting subscription, until the waiting subscription is rejected or allowed.
	WaitOnPermit(ctx context.Context, sub *appsapi.Subscription) *Status

	// RunPreBindPlugins runs the set of configured PreBind plugins. It returns
	// *Status and its code is set to non-success if any of the plugins returns
	// anything but Success. If the Status code is Unschedulable, it is
	// considered as a scheduling check failure, otherwise, it is considered as an
	// internal error. In either case the subscription is not going to be bound.
	RunPreBindPlugins(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status

	// RunPostBindPlugins runs the set of configured PostBind plugins.
	RunPostBindPlugins(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string)

	// RunBindPlugins runs the set of configured Bind plugins. A Bind plugin may choose
	// whether or not to handle the given subscription. If a Bind plugin chooses to skip the
	// binding, it should return code=5("skip") status. Otherwise, it should return "Error"
	// or "Success". If none of the plugins handled binding, RunBindPlugins returns
	// code=5("skip") status.
	RunBindPlugins(ctx context.Context, sub *appsapi.Subscription, namespacedClusters []string) *Status

	// HasFilterPlugins returns true if at least one Filter plugin is defined.
	HasFilterPlugins() bool

	// HasPostFilterPlugins returns true if at least one PostFilter plugin is defined.
	HasPostFilterPlugins() bool

	// HasScorePlugins returns true if at least one Score plugin is defined.
	HasScorePlugins() bool

	// ListPlugins returns a map of extension point name to list of configured Plugins.
	ListPlugins() *schedulerapis.Plugins

	// ProfileName returns the profile name associated to this framework.
	ProfileName() string
}

// Handle provides data and some tools that plugins can use. It is
// passed to the plugin factories at the time of plugin initialization. Plugins
// must store and use this handle to call framework functions.
type Handle interface {
	// PluginsRunner abstracts operations to run some plugins.
	PluginsRunner

	// IterateOverWaitingSubscriptions acquires a read lock and iterates over the WaitingSubscriptions map.
	IterateOverWaitingSubscriptions(callback func(WaitingSubscription))

	// GetWaitingSubscription returns a waiting subscription given its UID.
	GetWaitingSubscription(uid types.UID) WaitingSubscription

	// RejectWaitingSubscription rejects a waiting subscription given its UID.
	RejectWaitingSubscription(uid types.UID)

	// ClientSet returns a clusternet clientSet.
	ClientSet() clusternet.Interface

	// KubeConfig returns the raw kubeconfig.
	KubeConfig() *restclient.Config

	// EventRecorder returns an event recorder.
	EventRecorder() record.EventRecorder

	SharedInformerFactory() informers.SharedInformerFactory

	// Parallelizer returns a parallelizer holding parallelism for scheduler.
	Parallelizer() parallelize.Parallelizer
}

// PostFilterResult wraps needed info for scheduler framework to act upon PostFilter phase.
type PostFilterResult struct {
	NominatedNamespacedClusters []string
}

// PluginsRunner abstracts operations to run some plugins.
// This is used by preemption PostFilter plugins when evaluating the feasibility of
// scheduling the subscription on clusters when certain running subscriptions get evicted.
type PluginsRunner interface {
	// RunPreScorePlugins runs the set of configured PreScore plugins. If any
	// of these plugins returns any status other than "Success", the given subscription is rejected.
	RunPreScorePlugins(context.Context, *appsapi.Subscription, []*clusterapi.ManagedCluster) *Status

	// RunScorePlugins runs the set of configured Score plugins. It returns a map that
	// stores for each Score plugin name the corresponding ClusterScoreList(s).
	// It also returns *Status, which is set to non-success if any of the plugins returns
	// a non-success status.
	RunScorePlugins(context.Context, *appsapi.Subscription, []*clusterapi.ManagedCluster) (PluginToClusterScores, *Status)

	// RunFilterPlugins runs the set of configured Filter plugins for subscription on
	// the given cluster.
	RunFilterPlugins(context.Context, *appsapi.Subscription, *clusterapi.ManagedCluster) PluginToStatus
}
