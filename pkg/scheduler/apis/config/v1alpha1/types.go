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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SchedulerConfiguration configures a scheduler
type SchedulerConfiguration struct {
	metav1.TypeMeta `json:",inline"`

	// Profiles are scheduling profiles that clusternet-scheduler supports. subscription can
	// choose to be scheduled under a particular profile by setting its associated
	// scheduler name. subscription that don't specify any scheduler name are scheduled
	// with the "default" profile, if present here.
	Profiles []SchedulerProfile `json:"profiles,omitempty"`
}

// SchedulerProfile is a scheduling profile.
type SchedulerProfile struct {
	// SchedulerName is the name of the scheduler associated to this profile.
	// If SchedulerName matches with the pod's "spec.schedulerName", then the pod
	// is scheduled with this profile.
	SchedulerName *string `json:"schedulerName,omitempty"`

	// Plugins specify the set of plugins that should be enabled or disabled.
	// Enabled plugins are the ones that should be enabled in addition to the
	// default plugins. Disabled plugins are any of the default plugins that
	// should be disabled.
	// When no enabled or disabled plugin is specified for an extension point,
	// default plugins for that extension point will be used if there is any.
	// If a QueueSort plugin is specified, the same QueueSort Plugin and
	// PluginConfig must be specified for all profiles.
	Plugins *Plugins `json:"plugins,omitempty"`

	// PluginConfig is an optional set of custom plugin arguments for each plugin.
	// Omitting config args for a plugin is equivalent to using the default config
	// for that plugin.
	PluginConfig []PluginConfig `json:"pluginConfig,omitempty"`
}

// PluginConfig specifies arguments that should be passed to a plugin at the time of initialization.
// A plugin that is invoked at multiple extension points is initialized once. Args can have arbitrary structure.
// It is up to the plugin to process these Args.
type PluginConfig struct {
	// Name defines the name of plugin being configured
	Name string `json:"name,omitempty"`
	// Args defines the arguments passed to the plugins at the time of initialization. Args can have arbitrary structure.
	Args runtime.RawExtension `json:"args,omitempty"`
}

// Plugins include multiple extension points. When specified, the list of plugins for
// a particular extension point are the only ones enabled. If an extension point is
// omitted from the config, then the default set of plugins is used for that extension point.
// Enabled plugins are called in the order specified here, after default plugins. If they need to
// be invoked before default plugins, default plugins must be disabled and re-enabled here in desired order.
type Plugins struct {
	// PreFilter is a list of plugins that should be invoked at "PreFilter" extension point of the scheduling framework.
	PreFilter PluginSet `json:"preFilter,omitempty"`

	// Filter is a list of plugins that should be invoked when filtering out clusters that cannot run the Subscription.
	Filter PluginSet `json:"filter,omitempty"`

	// PostFilter is a list of plugins that are invoked after filtering phase, no matter whether filtering succeeds or not.
	PostFilter PluginSet `json:"postFilter,omitempty"`

	// PrePredict is a list of plugins that are invoked before predicting.
	PrePredict PluginSet `json:"prePredict,omitempty"`

	// Predict is a list of plugins that should be invoked when predicting max available replicas for clusters that have passed the filtering phase.
	Predict PluginSet `json:"predict,omitempty"`

	// PreScore is a list of plugins that are invoked before scoring.
	PreScore PluginSet `json:"preScore,omitempty"`

	// Score is a list of plugins that should be invoked when ranking clusters that have passed the filtering phase.
	Score PluginSet `json:"score,omitempty"`

	// PreAssign is a list of plugins that are invoked before assigning.
	PreAssign PluginSet `json:"preAssign,omitempty"`

	// Assign is a list of plugins that should be invoked when assigning replicas.
	Assign PluginSet `json:"assign,omitempty"`

	// Reserve is a list of plugins invoked when reserving/unreserving resources
	// after a cluster is assigned to run the Subscription.
	Reserve PluginSet `json:"reserve,omitempty"`

	// Permit is a list of plugins that control binding of a Subscription. These plugins can prevent or delay binding of a Subscription.
	Permit PluginSet `json:"permit,omitempty"`

	// PreBind is a list of plugins that should be invoked before a Subscription is bound.
	PreBind PluginSet `json:"preBind,omitempty"`

	// Bind is a list of plugins that should be invoked at "Bind" extension point of the scheduling framework.
	// The scheduler call these plugins in order. Scheduler skips the rest of these plugins as soon as one returns success.
	Bind PluginSet `json:"bind,omitempty"`

	// PostBind is a list of plugins that should be invoked after a Subscription is successfully bound.
	PostBind PluginSet `json:"postBind,omitempty"`
}

// PluginSet specifies enabled and disabled plugins for an extension point.
// If an array is empty, missing, or nil, default plugins at that extension point will be used.
type PluginSet struct {
	// Enabled specifies plugins that should be enabled in addition to default plugins.
	// These are called after default plugins and in the same order specified here.
	Enabled []Plugin `json:"enabled,omitempty"`
	// Disabled specifies default plugins that should be disabled.
	// When all default plugins need to be disabled, an array containing only one "*" should be provided.
	Disabled []Plugin `json:"disabled,omitempty"`
}

// Plugin specifies a plugin name and its weight when applicable. Weight is used only for Score plugins.
type Plugin struct {
	// Name defines the name of plugin
	Name string `json:"name,omitempty"`
	// Weight defines the weight of plugin, only used for Score plugins.
	Weight int32 `json:"weight,omitempty"`
}
