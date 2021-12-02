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

	"k8s.io/apimachinery/pkg/util/sets"

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
