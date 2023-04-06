/*
Copyright 2023 The Clusternet Authors.

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

package socket

import (
	compbasemetrics "k8s.io/component-base/metrics"
)

const (
	metricSubsystem = "socket_connections"
)

var (
	ConnectionCount = compbasemetrics.NewGauge(&compbasemetrics.GaugeOpts{
		Namespace:      category,
		Subsystem:      metricSubsystem,
		Name:           "count",
		Help:           "Number of socket connections",
		StabilityLevel: compbasemetrics.ALPHA,
	})

	ConnectionDurationSeconds = compbasemetrics.NewHistogram(
		&compbasemetrics.HistogramOpts{
			Namespace:      category,
			Subsystem:      metricSubsystem,
			Name:           "duration_seconds",
			Help:           "Active socket connection duration in seconds",
			Buckets:        compbasemetrics.ExponentialBuckets(60, 4, 8),
			StabilityLevel: compbasemetrics.ALPHA,
		},
	)
)
