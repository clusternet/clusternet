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
	predictedResult     = "predicted"
	unpredictableResult = "unpredictable"
	errorResult         = "error"
)

// RequirementPredictable can record a successful predicting attempt and the duration
// since `start`.
func RequirementPredictable(profile string, duration float64) {
	observePredictAttemptAndLatency(predictedResult, profile, duration)
}

// RequirementUnpredictable can record a predicting attempt for an unpredictable requirement
// and the duration since `start`.
func RequirementUnpredictable(profile string, duration float64) {
	observePredictAttemptAndLatency(unpredictableResult, profile, duration)
}

// RequirementPredictingError can record a predicting attempt that had an error and the
// duration since `start`.
func RequirementPredictingError(profile string, duration float64) {
	observePredictAttemptAndLatency(errorResult, profile, duration)
}

func observePredictAttemptAndLatency(result, profile string, duration float64) {
	e2ePredictorLatency.WithLabelValues(result, profile).Observe(duration)
	predictorLatency.WithLabelValues(result, profile).Observe(duration)
	predictorAttempts.WithLabelValues(result, profile).Inc()
}
