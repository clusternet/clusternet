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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/apis/config/types.go and modified

package config

import (
	"math"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SchedulerConfiguration configures a scheduler
type SchedulerConfiguration struct {
	metav1.TypeMeta

	// Profiles are scheduling profiles that clusternet-scheduler supports. subscription can
	// choose to be scheduled under a particular profile by setting its associated
	// scheduler name. subscription that don't specify any scheduler name are scheduled
	// with the "default" profile, if present here.
	Profiles []SchedulerProfile
}

// SchedulerProfile is a scheduling profile.
type SchedulerProfile struct {
	// SchedulerName is the name of the scheduler associated to this profile.
	// If SchedulerName matches with the pod's "spec.schedulerName", then the pod
	// is scheduled with this profile.
	SchedulerName string

	// Plugins specify the set of plugins that should be enabled or disabled.
	// Enabled plugins are the ones that should be enabled in addition to the
	// default plugins. Disabled plugins are any of the default plugins that
	// should be disabled.
	// When no enabled or disabled plugin is specified for an extension point,
	// default plugins for that extension point will be used if there is any.
	Plugins *Plugins

	// PluginConfig is an optional set of custom plugin arguments for each plugin.
	// Omitting config args for a plugin is equivalent to using the default config
	// for that plugin.
	PluginConfig []PluginConfig
}

// PluginConfig specifies arguments that should be passed to a plugin at the time of initialization.
// A plugin that is invoked at multiple extension points is initialized once. Args can have arbitrary structure.
// It is up to the plugin to process these Args.
type PluginConfig struct {
	// Name defines the name of plugin being configured
	Name string
	// Args defines the arguments passed to the plugins at the time of initialization. Args can have arbitrary structure.
	Args runtime.Object
}

// Plugins include multiple extension points. When specified, the list of plugins for
// a particular extension point are the only ones enabled. If an extension point is
// omitted from the config, then the default set of plugins is used for that extension point.
// Enabled plugins are called in the order specified here, after default plugins. If they need to
// be invoked before default plugins, default plugins must be disabled and re-enabled here in desired order.
type Plugins struct {
	// PreFilter is a list of plugins that should be invoked at "PreFilter" extension point of the scheduling framework.
	PreFilter PluginSet

	// Filter is a list of plugins that should be invoked when filtering out clusters that cannot run the Subscription.
	Filter PluginSet

	// PostFilter is a list of plugins that are invoked after filtering phase, no matter whether filtering succeeds or not.
	PostFilter PluginSet

	// PrePredict is a list of plugins that are invoked before predicting.
	PrePredict PluginSet

	// Predict is a list of plugins that should be invoked when predicting max available replicas for clusters that have passed the filtering phase.
	Predict PluginSet

	// PreScore is a list of plugins that are invoked before scoring.
	PreScore PluginSet

	// Score is a list of plugins that should be invoked when ranking clusters that have passed the filtering phase.
	Score PluginSet

	// PreAssign is a list of plugins that are invoked before assigning.
	PreAssign PluginSet

	// Assign is a list of plugins that should be invoked when assigning replicas.
	Assign PluginSet

	// Reserve is a list of plugins invoked when reserving/unreserving resources
	// after a cluster is assigned to run the Subscription.
	Reserve PluginSet

	// Permit is a list of plugins that control binding of a Subscription. These plugins can prevent or delay binding of a Subscription.
	Permit PluginSet

	// PreBind is a list of plugins that should be invoked before a Subscription is bound.
	PreBind PluginSet

	// Bind is a list of plugins that should be invoked at "Bind" extension point of the scheduling framework.
	// The scheduler call these plugins in order. Scheduler skips the rest of these plugins as soon as one returns success.
	Bind PluginSet

	// PostBind is a list of plugins that should be invoked after a Subscription is successfully bound.
	PostBind PluginSet
}

// PluginSet specifies enabled and disabled plugins for an extension point.
// If an array is empty, missing, or nil, default plugins at that extension point will be used.
type PluginSet struct {
	// Enabled specifies plugins that should be enabled in addition to default plugins.
	// These are called after default plugins and in the same order specified here.
	Enabled []Plugin
	// Disabled specifies default plugins that should be disabled.
	// When all default plugins need to be disabled, an array containing only one "*" should be provided.
	Disabled []Plugin
}

// Plugin specifies a plugin name and its weight when applicable. Weight is used only for Score plugins.
type Plugin struct {
	// Name defines the name of plugin
	Name string
	// Weight defines the weight of plugin, only used for Score plugins.
	Weight int32
}

/*
 * NOTE: The following variables and methods are intentionally left out of the staging mirror.
 */
const (
	// DefaultPercentageOfClustersToScore defines the percentage of clusters of all clusters
	// that once found feasible, the scheduler stops looking for more clusters.
	// A value of 0 means adaptive, meaning the scheduler figures out a proper default.
	DefaultPercentageOfClustersToScore = 0

	// MaxCustomPriorityScore is the max score UtilizationShapePoint expects.
	MaxCustomPriorityScore int64 = 10

	// MaxTotalScore is the maximum total score.
	MaxTotalScore int64 = math.MaxInt64

	// MaxWeight defines the max weight value allowed for custom PriorityPolicy
	MaxWeight = MaxTotalScore / MaxCustomPriorityScore
)

// Names returns the list of enabled plugin names.
func (p *Plugins) Names() []string {
	if p == nil {
		return nil
	}
	extensions := []PluginSet{
		p.PreFilter,
		p.Filter,
		p.PostFilter,
		p.Reserve,
		p.PrePredict,
		p.Predict,
		p.PreScore,
		p.Score,
		p.PreAssign,
		p.Assign,
		p.PreBind,
		p.Bind,
		p.PostBind,
		p.Permit,
	}
	n := sets.NewString()
	for _, e := range extensions {
		for _, pg := range e.Enabled {
			n.Insert(pg.Name)
		}
	}
	return n.List()
}
