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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/metrics/metrics.go and modified

package metrics

import (
	"sync"
	"time"

	"k8s.io/component-base/metrics"
	"k8s.io/component-base/metrics/legacyregistry"
)

const (
	// PredictorSubsystem - subsystem name used by predictor
	PredictorSubsystem = "clusternet_predictor"
)

// All the histogram based metrics have 1ms as size for the smallest bucket.
var (
	predictorAttempts = metrics.NewCounterVec(
		&metrics.CounterOpts{
			Subsystem:      PredictorSubsystem,
			Name:           "predictor_attempts_total",
			Help:           "Number of attempts to predictor requirements, by the result. 'Unpredictable' means a requirement could not be predicted, while 'error' means an internal predictor problem.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"result", "profile"})

	e2ePredictorLatency = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      PredictorSubsystem,
			Name:           "e2e_predictor_duration_seconds",
			Help:           "E2e predictor latency in seconds (predictor algorithm)",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		}, []string{"result", "profile"})

	predictorLatency = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem:      PredictorSubsystem,
			Name:           "predictor_attempt_duration_seconds",
			Help:           "Predictor attempt latency in seconds (predictor algorithm)",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.STABLE,
		}, []string{"result", "profile"})

	PredictorAlgorithmLatency = metrics.NewHistogram(
		&metrics.HistogramOpts{
			Subsystem:      PredictorSubsystem,
			Name:           "predictor_algorithm_duration_seconds",
			Help:           "Predictor algorithm latency in seconds",
			Buckets:        metrics.ExponentialBuckets(0.001, 2, 15),
			StabilityLevel: metrics.ALPHA,
		},
	)

	PredictorGoroutines = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      PredictorSubsystem,
			Name:           "predictor_goroutines",
			Help:           "Number of running goroutines split by the work they do.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"work"})

	FrameworkExtensionPointDuration = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem: PredictorSubsystem,
			Name:      "framework_extension_point_duration_seconds",
			Help:      "Latency for running all plugins of a specific extension point.",
			// Start with 0.1ms with the last bucket being [~200ms, Inf)
			Buckets:        metrics.ExponentialBuckets(0.0001, 2, 12),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"extension_point", "status", "profile"})

	PluginExecutionDuration = metrics.NewHistogramVec(
		&metrics.HistogramOpts{
			Subsystem: PredictorSubsystem,
			Name:      "plugin_execution_duration_seconds",
			Help:      "Duration for running a plugin at a specific extension point.",
			// Start with 0.01ms with the last bucket being [~22ms, Inf). We use a small factor (1.5)
			// so that we have better granularity since plugin latency is very sensitive.
			Buckets:        metrics.ExponentialBuckets(0.00001, 1.5, 20),
			StabilityLevel: metrics.ALPHA,
		},
		[]string{"plugin", "extension_point", "status"})

	metricsList = []metrics.Registerable{
		predictorAttempts,
		e2ePredictorLatency,
		PredictorAlgorithmLatency,
		FrameworkExtensionPointDuration,
		PluginExecutionDuration,
		PredictorGoroutines,
	}
)

var registerMetrics sync.Once

// Register all metrics.
func Register() {
	// Register the metrics.
	registerMetrics.Do(func() {
		RegisterMetrics(metricsList...)
	})
}

// RegisterMetrics registers a list of metrics.
// This function is exported because it is intended to be used by out-of-tree plugins to register their custom metrics.
func RegisterMetrics(extraMetrics ...metrics.Registerable) {
	for _, metric := range extraMetrics {
		legacyregistry.MustRegister(metric)
	}
}

// SinceInSeconds gets the time since the specified start in seconds.
func SinceInSeconds(start time.Time) float64 {
	return time.Since(start).Seconds()
}
