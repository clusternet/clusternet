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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/metrics/profile_metrics.go and modified
// This file contains helpers for metrics that are associated to a profile.

package metrics

var (
	estimateResult    = "estimated"
	inestimableResult = "inestimable"
	errorResult       = "error"
)

// RequirementEstimated can record a successful estimating attempt and the duration
// since `start`.
func RequirementEstimated(profile string, duration float64) {
	observeEstimateAttemptAndLatency(estimateResult, profile, duration)
}

// RequirementInestimable can record a estimating attempt for an inestimable requirement
// and the duration since `start`.
func RequirementInestimable(profile string, duration float64) {
	observeEstimateAttemptAndLatency(inestimableResult, profile, duration)
}

// RequirementEstimateError can record an estimating attempt that had an error and the
// duration since `start`.
func RequirementEstimateError(profile string, duration float64) {
	observeEstimateAttemptAndLatency(errorResult, profile, duration)
}

func observeEstimateAttemptAndLatency(result, profile string, duration float64) {
	e2eEstimatorLatency.WithLabelValues(result, profile).Observe(duration)
	estimatorLatency.WithLabelValues(result, profile).Observe(duration)
	estimatorAttempts.WithLabelValues(result, profile).Inc()
}
