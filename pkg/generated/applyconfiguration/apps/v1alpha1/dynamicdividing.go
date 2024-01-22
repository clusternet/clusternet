/*
Copyright The Clusternet Authors.

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
// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

// DynamicDividingApplyConfiguration represents an declarative configuration of the DynamicDividing type for use
// with apply.
type DynamicDividingApplyConfiguration struct {
	Strategy                  *v1alpha1.DynamicDividingStrategy `json:"strategy,omitempty"`
	TopologySpreadConstraints []v1.TopologySpreadConstraint     `json:"topologySpreadConstraints,omitempty"`
	PreferredClusters         []v1.PreferredSchedulingTerm      `json:"preferredClusters,omitempty"`
	MinClusters               *int32                            `json:"minClusters,omitempty"`
	MaxClusters               *int32                            `json:"maxClusters,omitempty"`
}

// DynamicDividingApplyConfiguration constructs an declarative configuration of the DynamicDividing type for use with
// apply.
func DynamicDividing() *DynamicDividingApplyConfiguration {
	return &DynamicDividingApplyConfiguration{}
}

// WithStrategy sets the Strategy field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Strategy field is set to the value of the last call.
func (b *DynamicDividingApplyConfiguration) WithStrategy(value v1alpha1.DynamicDividingStrategy) *DynamicDividingApplyConfiguration {
	b.Strategy = &value
	return b
}

// WithTopologySpreadConstraints adds the given value to the TopologySpreadConstraints field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the TopologySpreadConstraints field.
func (b *DynamicDividingApplyConfiguration) WithTopologySpreadConstraints(values ...v1.TopologySpreadConstraint) *DynamicDividingApplyConfiguration {
	for i := range values {
		b.TopologySpreadConstraints = append(b.TopologySpreadConstraints, values[i])
	}
	return b
}

// WithPreferredClusters adds the given value to the PreferredClusters field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the PreferredClusters field.
func (b *DynamicDividingApplyConfiguration) WithPreferredClusters(values ...v1.PreferredSchedulingTerm) *DynamicDividingApplyConfiguration {
	for i := range values {
		b.PreferredClusters = append(b.PreferredClusters, values[i])
	}
	return b
}

// WithMinClusters sets the MinClusters field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the MinClusters field is set to the value of the last call.
func (b *DynamicDividingApplyConfiguration) WithMinClusters(value int32) *DynamicDividingApplyConfiguration {
	b.MinClusters = &value
	return b
}

// WithMaxClusters sets the MaxClusters field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the MaxClusters field is set to the value of the last call.
func (b *DynamicDividingApplyConfiguration) WithMaxClusters(value int32) *DynamicDividingApplyConfiguration {
	b.MaxClusters = &value
	return b
}
