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

package apis

import (
	"math"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/clusternet/clusternet/pkg/utils"
)

// SchedulerConfiguration configures a scheduler
type SchedulerConfiguration struct {
	metav1.TypeMeta

	// Profiles are scheduling profiles that clusternet-scheduler supports. subscription can
	// choose to be scheduled under a particular profile by setting its associated
	// scheduler name. subscription that don't specify any scheduler name are scheduled
	// with the "default" profile, if present here.
	Profiles []SchedulerProfile `yaml:"profiles,omitempty"`

	// PercentageOfClustersToScore is the percentage of all clusters that once found feasible
	// for running a subscription, the scheduler stops its search for more feasible clusters.
	// This helps improve scheduler's performance. Scheduler always tries to find
	// at least "minFeasibleClustersToFind" feasible clusters no matter what the value of this flag is.
	// Example: if the total size is 500 clusters and the value of this flag is 30,
	// then scheduler stops finding further feasible clusters once it finds 150 feasible clusters.
	// When the value is 0, default percentage (5%--50% based on the size of the clusters) of the
	// clusters will be scored. It is overridden by profile level PercentageOfClustersToScore.
	PercentageOfClustersToScore *int32 `json:"percentageOfClustersToScore,omitempty"`

	// PercentageOfClustersToTolerate is the percentage of all feasible clusters that once found not responsive
	// when scheduling a subscription.
	// Currently, this flag is only used in dynamic scheduling.
	// When the predictor fails to get result for a feasible cluster, the scheduler framework will tolerate such
	// exception and continue getting scheduling results for other feasible clusters.
	// The predicting replicas for those failed or unresponsive clusters will be set to nil (0).
	// This helps improve scheduler's performance on dynamic scheduling with predictors.
	// Example: if the number of feasible clusters for a subscription with dynamic scheduling strategy is 100 and the
	// value of this flag is 10, then the predictor in the scheduling algorithm will ignore at most 10 failed
	// predicting results. If there are more than 10 failures, this scheduling should be marked as failure.
	PercentageOfClustersToTolerate *int32 `json:"percentageOfClustersToTolerate,omitempty"`
}

// SchedulerProfile is a scheduling profile.
type SchedulerProfile struct {
	// SchedulerName is the name of the scheduler associated to this profile.
	// If SchedulerName matches with the pod's "spec.schedulerName", then the pod
	// is scheduled with this profile.
	SchedulerName string `yaml:"schedulerName,omitempty"`

	// Plugins specify the set of plugins that should be enabled or disabled.
	// Enabled plugins are the ones that should be enabled in addition to the
	// default plugins. Disabled plugins are any of the default plugins that
	// should be disabled.
	// When no enabled or disabled plugin is specified for an extension point,
	// default plugins for that extension point will be used if there is any.
	Plugins *Plugins `yaml:"plugins,omitempty"`

	// PluginConfig is an optional set of custom plugin arguments for each plugin.
	// Omitting config args for a plugin is equivalent to using the default config
	// for that plugin.
	PluginConfig []PluginConfig `yaml:"pluginConfig,omitempty"`
}

// PluginConfig specifies arguments that should be passed to a plugin at the time of initialization.
// A plugin that is invoked at multiple extension points is initialized once. Args can have arbitrary structure.
// It is up to the plugin to process these Args.
type PluginConfig struct {
	// Name defines the name of plugin being configured
	Name string `yaml:"name,omitempty"`
	// Args defines the arguments passed to the plugins at the time of initialization. Args can have arbitrary structure.
	Args *RawExtension `yaml:"args,omitempty"`
}

// RawExtension is used to hold extensions in external versions.
type RawExtension struct {
	Raw []byte `yaml:"raw,omitempty"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (r *RawExtension) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var tmp interface{}
	if err := unmarshal(&tmp); err != nil {
		return err
	}
	var err error
	r.Raw, err = utils.Marshal(tmp)
	return err
}

// GetObjectKind returns a schema object.
func (r *RawExtension) GetObjectKind() schema.ObjectKind {
	return nil
}

// DeepCopyObject returns a generically typed copy of an object
func (r *RawExtension) DeepCopyObject() runtime.Object {
	return nil
}

// Plugins include multiple extension points. When specified, the list of plugins for
// a particular extension point are the only ones enabled. If an extension point is
// omitted from the config, then the default set of plugins is used for that extension point.
// Enabled plugins are called in the order specified here, after default plugins. If they need to
// be invoked before default plugins, default plugins must be disabled and re-enabled here in desired order.
type Plugins struct {
	// PreFilter is a list of plugins that should be invoked at "PreFilter" extension point of the scheduling framework.
	PreFilter PluginSet `yaml:"preFilter,omitempty"`

	// Filter is a list of plugins that should be invoked when filtering out clusters that cannot run the Subscription.
	Filter PluginSet `yaml:"filter,omitempty"`

	// PostFilter is a list of plugins that are invoked after filtering phase, no matter whether filtering succeeds or not.
	PostFilter PluginSet `yaml:"postFilter,omitempty"`

	// PrePredict is a list of plugins that are invoked before predicting.
	PrePredict PluginSet `yaml:"prePredict,omitempty"`

	// Predict is a list of plugins that should be invoked when predicting max available replicas for clusters that have passed the filtering phase.
	Predict PluginSet `yaml:"predict,omitempty"`

	// PreScore is a list of plugins that are invoked before scoring.
	PreScore PluginSet `yaml:"preScore,omitempty"`

	// Score is a list of plugins that should be invoked when ranking clusters that have passed the filtering phase.
	Score PluginSet `yaml:"score,omitempty"`

	// PreAssign is a list of plugins that are invoked before assigning.
	PreAssign PluginSet `yaml:"preAssign,omitempty"`

	// Assign is a list of plugins that should be invoked when assigning replicas.
	Assign PluginSet `yaml:"assign,omitempty"`

	// Reserve is a list of plugins invoked when reserving/unreserving resources
	// after a cluster is assigned to run the Subscription.
	Reserve PluginSet `yaml:"reserve,omitempty"`

	// Permit is a list of plugins that control binding of a Subscription. These plugins can prevent or delay binding of a Subscription.
	Permit PluginSet `yaml:"permit,omitempty"`

	// PreBind is a list of plugins that should be invoked before a Subscription is bound.
	PreBind PluginSet `yaml:"preBind,omitempty"`

	// Bind is a list of plugins that should be invoked at "Bind" extension point of the scheduling framework.
	// The scheduler call these plugins in order. Scheduler skips the rest of these plugins as soon as one returns success.
	Bind PluginSet `yaml:"bind,omitempty"`

	// PostBind is a list of plugins that should be invoked after a Subscription is successfully bound.
	PostBind PluginSet `yaml:"postBind,omitempty"`
}

// PluginSet specifies enabled and disabled plugins for an extension point.
// If an array is empty, missing, or nil, default plugins at that extension point will be used.
type PluginSet struct {
	// Enabled specifies plugins that should be enabled in addition to default plugins.
	// These are called after default plugins and in the same order specified here.
	Enabled []Plugin `yaml:"enabled,omitempty"`
	// Disabled specifies default plugins that should be disabled.
	// When all default plugins need to be disabled, an array containing only one "*" should be provided.
	Disabled []Plugin `yaml:"disabled,omitempty"`
}

// Plugin specifies a plugin name and its weight when applicable. Weight is used only for Score plugins.
type Plugin struct {
	// Name defines the name of plugin
	Name string `yaml:"name,omitempty"`
	// Weight defines the weight of plugin, only used for Score plugins.
	Weight int32 `yaml:"weight,omitempty"`
}

/*
 * NOTE: The following variables and methods are intentionally left out of the staging mirror.
 */
const (
	// DefaultPercentageOfClustersToScore defines the percentage of clusters of all clusters
	// that once found feasible, the scheduler stops looking for more clusters.
	// A value of 0 means adaptive, meaning the scheduler figures out a proper default.
	DefaultPercentageOfClustersToScore = int32(0)

	// DefaultPercentageOfClustersToTolerate defines the percentage of clusters of all feasible clusters
	// that once found not responsive, the scheduler tolerates the failure and continues getting predicting results
	// for other clusters.
	DefaultPercentageOfClustersToTolerate = int32(10)

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
