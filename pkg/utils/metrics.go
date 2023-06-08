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

package utils

import (
	"net/http"
	goruntime "runtime"

	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/apiserver/pkg/server/mux"
	"k8s.io/apiserver/pkg/server/routes"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/configz"
	"k8s.io/component-base/logs"
	"k8s.io/component-base/metrics/features"
	"k8s.io/component-base/metrics/legacyregistry"
	"k8s.io/component-base/metrics/prometheus/slis"
	controllermanageroptions "k8s.io/controller-manager/options"
)

// NewHealthzAndMetricsHandler creates a healthz server from the config, and will also
// embed the metrics handler.
func NewHealthzAndMetricsHandler(name string, debuggingOptions *controllermanageroptions.DebuggingOptions, checks ...healthz.HealthChecker) http.Handler {
	pathRecorderMux := mux.NewPathRecorderMux(name)
	healthz.InstallHandler(pathRecorderMux, checks...)
	configz.InstallHandler(pathRecorderMux)
	pathRecorderMux.Handle("/metrics", legacyregistry.HandlerWithReset())
	if utilfeature.DefaultFeatureGate.Enabled(features.ComponentSLIs) {
		slis.SLIMetricsWithReset{}.Install(pathRecorderMux)
	}
	if debuggingOptions.EnableProfiling {
		routes.Profiling{}.Install(pathRecorderMux)
		if debuggingOptions.EnableContentionProfiling {
			goruntime.SetBlockProfileRate(1)
		}
		routes.DebugFlags{}.Install(pathRecorderMux, "v", routes.StringFlagPutHandler(logs.GlogSetter))
	}
	return pathRecorderMux
}
