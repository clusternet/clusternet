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

package interfaces

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

// Diagnosis records the details to diagnose a scheduling failure.
type Diagnosis struct {
	ClusterToStatusMap   ClusterToStatusMap
	UnschedulablePlugins sets.String
}

// FitError describes a fit error of a subscription.
type FitError struct {
	Subscription   *appsapi.Subscription
	NumAllClusters int
	Diagnosis      Diagnosis
}

// TargetClusters represents the scheduling result.
type TargetClusters struct {
	// Namespaced names of targeted clusters that Subscription binds to.
	BindingClusters []string

	// Desired replicas of targeted clusters for each feed.
	Replicas map[string][]int32
}

func NewTargetClusters(bindingClusters []string, replicas map[string][]int32) *TargetClusters {
	return &TargetClusters{BindingClusters: bindingClusters, Replicas: replicas}
}

func (t TargetClusters) Len() int {
	return len(t.BindingClusters)
}

func (t TargetClusters) Less(i, j int) bool {
	return t.BindingClusters[i] < t.BindingClusters[j]
}

func (t TargetClusters) Swap(i, j int) {
	t.BindingClusters[i], t.BindingClusters[j] = t.BindingClusters[j], t.BindingClusters[i]
	for _, replicas := range t.Replicas {
		if len(replicas) == len(t.BindingClusters) {
			replicas[i], replicas[j] = replicas[j], replicas[i]
		}
	}
}

func (t *TargetClusters) DeepCopy() *TargetClusters {
	if t == nil {
		return nil
	}
	obj := new(TargetClusters)
	if t.BindingClusters != nil {
		in, out := &t.BindingClusters, &obj.BindingClusters
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	if t.Replicas != nil {
		in, out := &t.Replicas, &obj.Replicas
		*out = make(map[string][]int32, len(*in))
		for key, val := range *in {
			var outVal []int32
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = make([]int32, len(*in))
				copy(*out, *in)
			}
			(*out)[key] = outVal
		}
	}
	return obj
}

// If b.Replicas have feed more then one , use this method, it works good
// https://github.com/clusternet/clusternet/blob/v0.14.0/pkg/scheduler/framework/interfaces/types.go#L100
// MergeOneFeed use for merge two TargetClusters when b.Replicas only one feed
func (t *TargetClusters) MergeOneFeed(b *TargetClusters) {
	klog.Info("merge one feed : ", b)
	if b == nil || len(b.BindingClusters) == 0 {
		return
	}
	if t.Replicas == nil {
		t.Replicas = make(map[string][]int32)
	}
	// If the feed replicas does not exist in the former target clusters, we initialize them as an empty one.
	for feed, replicas := range b.Replicas {
		if _, exist := t.Replicas[feed]; exist {
			continue
		}
		if len(replicas) == 0 {
			t.Replicas[feed] = make([]int32, 0)
		} else {
			t.Replicas[feed] = make([]int32, len(t.BindingClusters))
		}
	}

	// transfer from cluster to index
	m := make(map[string]int)
	for i, cluster := range t.BindingClusters {
		m[cluster] = i
	}

	// only one feed in b.Replicas, get feed and replicas by loop
	for feed, replicas := range b.Replicas {
		if len(replicas) == 0 {
			if _, isok := t.Replicas[feed]; !isok {
				t.Replicas[feed] = make([]int32, 0)
			}
			return
		}
		for bi, cluster := range b.BindingClusters {
			// same cluster , use b binding cluster index get replica, and assign to t replicas
			if ti, exist := m[cluster]; exist {
				if len(t.Replicas[feed]) != len(t.BindingClusters) {
					t.Replicas[feed] = make([]int32, len(t.BindingClusters))
					t.Replicas[feed][ti] = replicas[bi]
					continue
				}
				t.Replicas[feed][ti] += replicas[bi]
			} else {
				// new cluster, add cluster to t binding cluster
				t.BindingClusters = append(t.BindingClusters, cluster)
				m[cluster] = len(t.BindingClusters) - 1
				// add 0 to exist feed but new cluster, if feed is nil, skip it
				for f := range t.Replicas {
					if f != feed {
						if len(t.Replicas[f]) != 0 {
							t.Replicas[f] = append(t.Replicas[f], 0)
						}
					} else {
						t.Replicas[f] = append(t.Replicas[f], b.Replicas[feed][bi])
					}
				}
			}
		}
	}
}

const (
	// NoClusterAvailableMsg is used to format message when no clusters available.
	NoClusterAvailableMsg = "0/%v clusters are available"
)

// Error returns detailed information of why the subscription failed to fit on each cluster
func (f *FitError) Error() string {
	reasons := make(map[string]int)
	for _, status := range f.Diagnosis.ClusterToStatusMap {
		for _, reason := range status.Reasons() {
			reasons[reason]++
		}
	}

	sortReasonsHistogram := func() []string {
		var reasonStrings []string
		for k, v := range reasons {
			reasonStrings = append(reasonStrings, fmt.Sprintf("%v %v", v, k))
		}
		sort.Strings(reasonStrings)
		return reasonStrings
	}
	reasonMsg := fmt.Sprintf(NoClusterAvailableMsg+": %v.", f.NumAllClusters, strings.Join(sortReasonsHistogram(), ", "))
	return reasonMsg
}

// SubscriptionInfo is a wrapper to a Subscription for tracking
// the timestamp when it's added to the queue or recording per subscription metrics.
type SubscriptionInfo struct {
	Subscription *appsapi.Subscription
	// The time subscription added to the scheduling queue.
	Timestamp time.Time
	// Number of schedule attempts before successfully scheduled.
	// It's used to record the # attempts metric.
	Attempts int
	// The time when the subscription is added to the queue for the first time. The subscription may be added
	// back to the queue multiple times before it's successfully scheduled.
	// It shouldn't be updated once initialized. It's used to record the e2e scheduling
	// latency for a subscription.
	InitialAttemptTimestamp time.Time
}

// DeepCopy returns a deep copy of the SubscriptionInfo object.
func (subInfo *SubscriptionInfo) DeepCopy() *SubscriptionInfo {
	return &SubscriptionInfo{
		Subscription:            subInfo.Subscription.DeepCopy(),
		Timestamp:               subInfo.Timestamp,
		Attempts:                subInfo.Attempts,
		InitialAttemptTimestamp: subInfo.InitialAttemptTimestamp,
	}
}

// LessFunc is the function to sort subscription info
type LessFunc func(subInfo1, subInfo2 *SubscriptionInfo) bool

// Less is the function used by the activeQ heap algorithm to sort subscriptions.
// It sorts subscriptions based on their priority. When priorities are equal, it uses
// SubscriptionInfo.timestamp.
func Less(sInfo1, sInfo2 *SubscriptionInfo) bool {
	s1 := subscriptionPriority(sInfo1.Subscription)
	s2 := subscriptionPriority(sInfo2.Subscription)
	return (s1 > s2) || (s1 == s2 && sInfo1.Timestamp.Before(sInfo2.Timestamp))
}

// subscriptionPriority returns priority of the given subscription.
func subscriptionPriority(sub *appsapi.Subscription) int32 {
	if sub.Spec.Priority != nil {
		return *sub.Spec.Priority
	}
	// When priority of a running subscription is nil, it means it was created at a time
	// that there was no global default priority class and the priority class
	// name of the subscription was empty. So, we resolve to the static default priority.
	return 0
}
