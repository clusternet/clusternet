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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/generic_scheduler.go and modified

package algorithm

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"
	utiltrace "k8s.io/utils/trace"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	schedulercache "github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/scheduler/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// minFeasibleClustersToFind is the minimum number of clusters that would be scored
	// in each scheduling cycle. This is a semi-arbitrary value to ensure that a
	// certain minimum of clusters are checked for feasibility. This in turn helps
	// ensure a minimum level of spreading.
	minFeasibleClustersToFind = 100
	// minFeasibleClustersPercentageToFind is the minimum percentage of clusters that
	// would be scored in each scheduling cycle. This is a semi-arbitrary value
	// to ensure that a certain minimum of clusters are checked for feasibility.
	// This in turn helps ensure a minimum level of spreading.
	minFeasibleClustersPercentageToFind = 5
)

// ErrNoClustersAvailable is used to describe the error that no clusters available to schedule subscriptions.
var ErrNoClustersAvailable = fmt.Errorf("no clusters available to schedule subscriptions")

type genericScheduler struct {
	cache                       schedulercache.Cache
	percentageOfClustersToScore int32
	nextStartClusterIndex       int
}

// Schedule tries to schedule the given subscription to multiple clusters.
// If it succeeds, it will return the namespaced names of ManagedClusters.
// If it fails, it will return a FitError error with reasons.
func (g *genericScheduler) Schedule(ctx context.Context, fwk framework.Framework, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory) (result ScheduleResult, err error) {
	trace := utiltrace.New("Scheduling", utiltrace.Field{Key: "namespace", Value: sub.Namespace}, utiltrace.Field{Key: "name", Value: sub.Name})
	defer trace.LogIfLong(100 * time.Millisecond)

	if g.cache.NumClusters() == 0 {
		return result, ErrNoClustersAvailable
	}

	// Step 1: Filter clusters.
	feasibleClusters, diagnosis, err := g.findClustersThatFitSubscription(ctx, fwk, state, sub)
	if err != nil {
		return result, err
	}
	trace.Step("Computing predicates done")

	if len(feasibleClusters) == 0 {
		return result, &framework.FitError{
			Subscription:   sub,
			NumAllClusters: g.cache.NumClusters(),
			Diagnosis:      diagnosis,
		}
	}

	// Step 2: Predict max available replicas if necessary.
	availableList, err := predictReplicas(ctx, fwk, state, sub, finv, feasibleClusters)
	if err != nil {
		return result, err
	}

	// Step 3: Prioritize clusters.
	priorityList, err := prioritizeClusters(ctx, fwk, state, sub, feasibleClusters, availableList)
	if err != nil {
		return result, err
	}

	// Step 4: Subgroup clusters.
	subgroupList, err := g.subgroupClusters(sub, priorityList)
	if err != nil {
		return result, err
	}

	clusters, err := g.selectClusters(ctx, state, subgroupList, fwk, sub, finv)
	trace.Step("Prioritizing done")

	return ScheduleResult{
		SuggestedClusters: clusters,
		EvaluatedClusters: len(feasibleClusters) + len(diagnosis.ClusterToStatusMap),
		FeasibleClusters:  len(feasibleClusters),
	}, err
}

func predictReplicas(ctx context.Context, fwk framework.Framework, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, clusters []*clusterapi.ManagedCluster) (framework.ClusterScoreList, error) {
	availableList := make(framework.ClusterScoreList, len(clusters))
	for i := range clusters {
		availableList[i] = framework.ClusterScore{
			NamespacedName: klog.KObj(clusters[i]).String(),
		}
	}

	// If not dividing, finv does not exist.
	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return availableList, nil
	}

	// Initialize.
	for i := range availableList {
		availableList[i].MaxAvailableReplicas = make(framework.FeedReplicas, len(finv.Spec.Feeds))
	}

	if sub.Spec.DividingScheduling == nil || sub.Spec.DividingScheduling.Type == appsapi.StaticReplicaDividingType || !fwk.HasPredictPlugins() {
		return availableList, nil
	}

	// Run PrePredict plugins.
	prePredictStatus := fwk.RunPrePredictPlugins(ctx, state, sub, finv, clusters)
	if !prePredictStatus.IsSuccess() {
		return nil, prePredictStatus.AsError()
	}

	// Run Predict plugins.
	availableList, predictStatus := fwk.RunPredictPlugins(ctx, state, sub, finv, clusters, availableList)
	if !predictStatus.IsSuccess() {
		return nil, predictStatus.AsError()
	}

	if klog.V(10).Enabled() {
		for i := range availableList {
			klog.V(10).InfoS("Predicting cluster's max available replicas for subscription", "subscription", klog.KObj(sub), "cluster", availableList[i].NamespacedName, "max available replicas", availableList[i].MaxAvailableReplicas)
		}
	}
	return availableList, nil
}

// selectClusters takes a prioritized list of clusters and then picks a fraction of clusters
// in a reservoir sampling manner from the clusters that had the highest score.
func (g *genericScheduler) selectClusters(ctx context.Context, state *framework.CycleState, clusterScoreList framework.ClusterScoreList, fwk framework.Framework, sub *appsapi.Subscription, finv *appsapi.FeedInventory) (framework.TargetClusters, error) {
	if len(clusterScoreList) == 0 {
		return framework.TargetClusters{}, fmt.Errorf("empty clusterScoreList")
	}

	var selected framework.TargetClusters
	for _, clusterScore := range clusterScoreList {
		// TODO: sampling with scores
		selected.BindingClusters = append(selected.BindingClusters, clusterScore.NamespacedName)
	}

	if sub.Spec.SchedulingStrategy != appsapi.DividingSchedulingStrategyType {
		return selected, nil
	}

	// transfer available replicas only if dynamic dividing
	if sub.Spec.DividingScheduling != nil && sub.Spec.DividingScheduling.Type == appsapi.DynamicReplicaDividingType {
		selected.Replicas = make(map[string][]int32)
		// transfer to available replicas if necessary
		for index, feedOrder := range finv.Spec.Feeds {
			if feedOrder.DesiredReplicas == nil {
				continue
			}
			for _, clusterScore := range clusterScoreList {
				feedKey := utils.GetFeedKey(feedOrder.Feed)
				if clusterScore.MaxAvailableReplicas[index] == nil {
					return framework.TargetClusters{}, fmt.Errorf("unable to get replicas for feed %q in cluster %q", feedKey, clusterScore.NamespacedName)
				}
				selected.Replicas[feedKey] = append(selected.Replicas[feedKey], *clusterScore.MaxAvailableReplicas[index])
			}
		}
	}

	// Run PreAssign plugins.
	preAssignStatus := fwk.RunPreAssignPlugins(ctx, state, sub, finv, selected)
	if !preAssignStatus.IsSuccess() {
		return framework.TargetClusters{}, preAssignStatus.AsError()
	}

	// Run the Assign plugins.
	selected, assignStatus := fwk.RunAssignPlugins(ctx, state, sub, finv, selected)
	if !assignStatus.IsSuccess() {
		if assignStatus.Code() == framework.Error {
			return framework.TargetClusters{}, assignStatus.AsError()
		}
		return framework.TargetClusters{}, fmt.Errorf("assign status: %s, %v", assignStatus.Code().String(), assignStatus.Message())
	}

	// Run PostAssign plugins
	postAssignStatus := fwk.RunPostAssignPlugins(ctx, state, sub, finv, selected)
	if !postAssignStatus.IsSuccess() {
		return framework.TargetClusters{}, postAssignStatus.AsError()
	}

	return selected, nil
}

// numFeasibleClustersToFind returns the number of feasible clusters that once found, the scheduler stops
// its search for more feasible clusters.
func (g *genericScheduler) numFeasibleClustersToFind(numAllClusters int32, schedulingStrategy appsapi.SchedulingStrategyType) (numClusters int32) {
	if numAllClusters < minFeasibleClustersToFind || g.percentageOfClustersToScore >= 100 || schedulingStrategy == appsapi.ReplicaSchedulingStrategyType {
		return numAllClusters
	}

	adaptivePercentage := g.percentageOfClustersToScore
	if adaptivePercentage <= 0 {
		basePercentageOfClustersToScore := int32(50)
		adaptivePercentage = basePercentageOfClustersToScore - numAllClusters/125
		if adaptivePercentage < minFeasibleClustersPercentageToFind {
			adaptivePercentage = minFeasibleClustersPercentageToFind
		}
	}

	numClusters = numAllClusters * adaptivePercentage / 100
	if numClusters < minFeasibleClustersToFind {
		return minFeasibleClustersToFind
	}

	return numClusters
}

// Filters the clusters to find the ones that fit the subscription based on the framework filter plugins.
func (g *genericScheduler) findClustersThatFitSubscription(ctx context.Context, fwk framework.Framework, state *framework.CycleState, sub *appsapi.Subscription) ([]*clusterapi.ManagedCluster, framework.Diagnosis, error) {
	diagnosis := framework.Diagnosis{
		ClusterToStatusMap:   make(framework.ClusterToStatusMap),
		UnschedulablePlugins: sets.NewString(),
	}

	var allClusters []*clusterapi.ManagedCluster
	for _, subscriber := range sub.Spec.Subscribers {
		clusters, err := g.cache.List(subscriber.ClusterAffinity)
		if err != nil {
			return nil, diagnosis, err
		}
		allClusters = append(allClusters, clusters...)
	}
	allClusters = normalizedClusters(allClusters)
	// Return immediately if no clusters match the cluster affinity.
	if len(allClusters) == 0 {
		return allClusters, diagnosis, nil
	}

	// Run "prefilter" plugins.
	s := fwk.RunPreFilterPlugins(ctx, state, sub)
	if !s.IsSuccess() {
		if !s.IsUnschedulable() {
			return nil, diagnosis, s.AsError()
		}
		// All clusters will have the same status. Some non trivial refactoring is
		// needed to avoid this copy.
		for _, n := range allClusters {
			diagnosis.ClusterToStatusMap[klog.KObj(n).String()] = s
		}
		// Status satisfying IsUnschedulable() gets injected into diagnosis.UnschedulablePlugins.
		diagnosis.UnschedulablePlugins.Insert(s.FailedPlugin())
		return nil, diagnosis, nil
	}

	feasibleClusters, err := g.findClustersThatPassFilters(ctx, state, fwk, sub, diagnosis, allClusters)
	if err != nil {
		return nil, diagnosis, err
	}
	return feasibleClusters, diagnosis, nil
}

// findClustersThatPassFilters finds the clusters that fit the filter plugins.
func (g *genericScheduler) findClustersThatPassFilters(ctx context.Context, state *framework.CycleState, fwk framework.Framework, sub *appsapi.Subscription, diagnosis framework.Diagnosis, clusters []*clusterapi.ManagedCluster) ([]*clusterapi.ManagedCluster, error) {
	numClustersToFind := g.numFeasibleClustersToFind(int32(len(clusters)), sub.Spec.SchedulingStrategy)

	// Create feasible list with enough space to avoid growing it
	// and allow assigning.
	feasibleClusters := make([]*clusterapi.ManagedCluster, numClustersToFind)

	if !fwk.HasFilterPlugins() {
		length := len(clusters)
		for i := range feasibleClusters {
			feasibleClusters[i] = clusters[(g.nextStartClusterIndex+i)%length]
		}
		g.nextStartClusterIndex = (g.nextStartClusterIndex + len(feasibleClusters)) % length
		return feasibleClusters, nil
	}

	errCh := parallelize.NewErrorChannel()
	var statusesLock sync.Mutex
	var feasibleClustersLen int32
	ctx, cancel := context.WithCancel(ctx)
	checkCluster := func(i int) {
		// We check the clusters starting from where we left off in the previous scheduling cycle,
		// this is to make sure all clusters have the same chance of being examined across subscriptions.
		cluster := clusters[(g.nextStartClusterIndex+i)%len(clusters)]

		status := fwk.RunFilterPlugins(ctx, state, sub, cluster).Merge()
		if status.Code() == framework.Error {
			errCh.SendErrorWithCancel(status.AsError(), cancel)
			return
		}
		if status.IsSuccess() {
			length := atomic.AddInt32(&feasibleClustersLen, 1)
			if length > numClustersToFind {
				cancel()
				atomic.AddInt32(&feasibleClustersLen, -1)
			} else {
				feasibleClusters[length-1] = cluster
			}
		} else {
			statusesLock.Lock()
			diagnosis.ClusterToStatusMap[klog.KObj(cluster).String()] = status
			diagnosis.UnschedulablePlugins.Insert(status.FailedPlugin())
			statusesLock.Unlock()
		}
	}

	beginCheckCluster := time.Now()
	statusCode := framework.Success
	defer func() {
		// We record Filter extension point latency here instead of in framework.go because framework.RunFilterPlugins
		// function is called for each cluster, whereas we want to have an overall latency for all clusters per scheduling cycle.
		metrics.FrameworkExtensionPointDuration.WithLabelValues(runtime.Filter, statusCode.String(), fwk.ProfileName()).Observe(metrics.SinceInSeconds(beginCheckCluster))
	}()

	// Stops searching for more clusters once the configured number of feasible clusters
	// are found.
	fwk.Parallelizer().Until(ctx, len(clusters), checkCluster)
	processedClusters := int(feasibleClustersLen) + len(diagnosis.ClusterToStatusMap)
	g.nextStartClusterIndex = (g.nextStartClusterIndex + processedClusters) % len(clusters)

	feasibleClusters = feasibleClusters[:feasibleClustersLen]
	if err := errCh.ReceiveError(); err != nil {
		statusCode = framework.Error
		return nil, err
	}
	return feasibleClusters, nil
}

// prioritizeClusters prioritizes the clusters by running the score plugins,
// which return a score for each cluster from the call to RunScorePlugins().
// The scores from each plugin are added together to make the score for that cluster, then
// any extenders are run as well.
// All scores are finally combined (added) to get the total weighted scores of all clusters
func prioritizeClusters(ctx context.Context, fwk framework.Framework, state *framework.CycleState, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster, result framework.ClusterScoreList) (framework.ClusterScoreList, error) {
	// If no priority configs are provided, then all clusters will have a score of one.
	// This is required to generate the priority list in the required format
	if !fwk.HasScorePlugins() {
		for i := range result {
			result[i].Score = 1
		}
		return result, nil
	}

	// Run PreScore plugins.
	preScoreStatus := fwk.RunPreScorePlugins(ctx, state, sub, clusters)
	if !preScoreStatus.IsSuccess() {
		return nil, preScoreStatus.AsError()
	}

	// Run the Score plugins.
	scoresMap, scoreStatus := fwk.RunScorePlugins(ctx, state, sub, clusters)
	if !scoreStatus.IsSuccess() {
		return nil, scoreStatus.AsError()
	}

	if klog.V(10).Enabled() {
		for plugin, clusterScoreList := range scoresMap {
			for _, clusterScore := range clusterScoreList {
				klog.V(10).InfoS("Plugin scored cluster for subscription", "subscription", klog.KObj(sub), "plugin", plugin, "cluster", clusterScore.NamespacedName, "score", clusterScore.Score)
			}
		}
	}

	for i := range clusters {
		for j := range scoresMap {
			result[i].Score += scoresMap[j][i].Score
		}
	}
	sort.Sort(result)

	if klog.V(10).Enabled() {
		for i := range result {
			klog.V(10).InfoS("Calculated cluster's final score for subscription", "subscription", klog.KObj(sub), "cluster", result[i].NamespacedName, "score", result[i].Score)
		}
	}
	return result, nil
}

// subgroupClusters grouping the clusters by subgroups
func (g *genericScheduler) subgroupClusters(sub *appsapi.Subscription, clusterScoreList framework.ClusterScoreList) (framework.ClusterScoreList, error) {
	if sub.Spec.SchedulingBySubGroup == nil || !*sub.Spec.SchedulingBySubGroup {
		return clusterScoreList, nil
	}

	subgroup := make([]framework.ClusterScoreList, len(sub.Spec.Subscribers))
	result := make(framework.ClusterScoreList, 0)
	for i, subscriber := range sub.Spec.Subscribers {
		selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
		if err != nil {
			continue
		}
		for _, clusterScore := range clusterScoreList {
			cluster, err2 := g.cache.Get(clusterScore.NamespacedName)
			if err2 != nil {
				return nil, err2
			}
			if !selector.Matches(labels.Set(cluster.Labels)) {
				continue
			}
			subgroup[i] = append(subgroup[i], clusterScore)
		}

		if subscriber.SubGroupStrategy == nil {
			return nil, fmt.Errorf("subGroupStrategy filed can not be Empty")
		}
		minClusters := subscriber.SubGroupStrategy.MinClusters

		subgroupLen := int32(len(subgroup[i]))
		if minClusters > subgroupLen {
			minClusters = subgroupLen
		}
		subgroup[i] = subgroup[i][:minClusters]

		result = append(result, subgroup[i]...)
	}

	return result, nil
}

// NewGenericScheduler creates a genericScheduler object.
func NewGenericScheduler(cache schedulercache.Cache, percentageOfClustersToScore int32) ScheduleAlgorithm {
	return &genericScheduler{
		cache:                       cache,
		percentageOfClustersToScore: percentageOfClustersToScore,
	}
}

// normalizedClusters will remove duplicate clusters. Deleting clusters will be removed as well.
func normalizedClusters(clusters []*clusterapi.ManagedCluster) []*clusterapi.ManagedCluster {
	allKeys := make(map[string]bool)
	var uniqueClusters []*clusterapi.ManagedCluster
	for _, cluster := range clusters {
		if _, ok := allKeys[klog.KObj(cluster).String()]; !ok {
			if cluster.DeletionTimestamp != nil {
				continue
			}
			uniqueClusters = append(uniqueClusters, cluster)
			allKeys[klog.KObj(cluster).String()] = true
		}
	}
	return uniqueClusters
}
