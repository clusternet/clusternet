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

package algorithm

import (
	"context"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
)

// ScheduleAlgorithm is an interface implemented by things that know how to schedule resources to target
// managed clusters.
type ScheduleAlgorithm interface {
	Schedule(context.Context, framework.Framework, *framework.CycleState, *appsapi.Subscription, *appsapi.FeedInventory) (scheduleResult ScheduleResult, err error)
}

// ScheduleResult represents the result of one subscription scheduled. It will contain
// the final selected clusters, along with the selected intermediate information.
type ScheduleResult struct {
	// the scheduler suggest clusters (namespaced name)
	SuggestedClusters framework.TargetClusters
	// Number of clusters scheduler evaluated on one subscription scheduled
	EvaluatedClusters int
	// Number of feasible clusters on one subscription scheduled
	FeasibleClusters int
}
