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

// This file was copied from k8s.io/kubernetes/pkg/scheduler/framework/runtime/framework.go and modified

package runtime

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	schedulerapis "github.com/clusternet/clusternet/pkg/scheduler/apis"
	"github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// Filter is the name of the filter extension point.
	Filter = "Filter"
	// Specifies the maximum timeout a permit plugin can return.
	maxTimeout              = 15 * time.Minute
	preFilter               = "PreFilter"
	postFilter              = "PostFilter"
	prePredict              = "PrePredict"
	predict                 = "Predict"
	preScore                = "PreScore"
	score                   = "Score"
	preAssign               = "PreAssign"
	assign                  = "Assign"
	postAssign              = "PostAssign"
	scoreExtensionNormalize = "ScoreExtensionNormalize"
	preBind                 = "PreBind"
	bind                    = "Bind"
	postBind                = "PostBind"
	reserve                 = "Reserve"
	unreserve               = "Unreserve"
	permit                  = "Permit"
)

// frameworkImpl is the component responsible for initializing and running scheduler
// plugins.
type frameworkImpl struct {
	waitingSubscriptions *waitingSubscriptionsMap
	scorePluginWeight    map[string]int
	preFilterPlugins     []framework.PreFilterPlugin
	filterPlugins        []framework.FilterPlugin
	postFilterPlugins    []framework.PostFilterPlugin
	prePredictPlugins    []framework.PrePredictPlugin
	predictPlugins       []framework.PredictPlugin
	preScorePlugins      []framework.PreScorePlugin
	scorePlugins         []framework.ScorePlugin
	preAssignPlugins     []framework.PreAssignPlugin
	assignPlugins        []framework.AssignPlugin
	postAssignPlugins    []framework.PostAssignPlugin
	reservePlugins       []framework.ReservePlugin
	preBindPlugins       []framework.PreBindPlugin
	bindPlugins          []framework.BindPlugin
	postBindPlugins      []framework.PostBindPlugin
	permitPlugins        []framework.PermitPlugin

	clientSet       clusternet.Interface
	kubeConfig      *restclient.Config
	eventRecorder   record.EventRecorder
	informerFactory informers.SharedInformerFactory
	cache           cache.Cache

	metricsRecorder *metricsRecorder
	profileName     string

	parallelizer parallelize.Parallelizer

	// Indicates that RunFilterPlugins should accumulate all failed statuses and not return
	// after the first failure.
	runAllFilters bool

	percentageOfClustersToTolerate int32
}

// extensionPoint encapsulates desired and applied set of plugins at a specific extension
// point. This is used to simplify iterating over all extension points supported by the
// frameworkImpl.
type extensionPoint struct {
	// the set of plugins to be configured at this extension point.
	plugins *schedulerapis.PluginSet
	// a pointer to the slice storing plugins implementations that will run at this
	// extension point.
	slicePtr interface{}
}

func (f *frameworkImpl) getExtensionPoints(plugins *schedulerapis.Plugins) []extensionPoint {
	return []extensionPoint{
		{&plugins.PreFilter, &f.preFilterPlugins},
		{&plugins.Filter, &f.filterPlugins},
		{&plugins.PostFilter, &f.postFilterPlugins},
		{&plugins.Reserve, &f.reservePlugins},
		{&plugins.PrePredict, &f.prePredictPlugins},
		{&plugins.Predict, &f.predictPlugins},
		{&plugins.PreScore, &f.preScorePlugins},
		{&plugins.Score, &f.scorePlugins},
		{&plugins.PreAssign, &f.preAssignPlugins},
		{&plugins.Assign, &f.assignPlugins},
		{&plugins.PreBind, &f.preBindPlugins},
		{&plugins.Bind, &f.bindPlugins},
		{&plugins.PostBind, &f.postBindPlugins},
		{&plugins.Permit, &f.permitPlugins},
	}
}

type frameworkOptions struct {
	clientSet       clusternet.Interface
	kubeConfig      *restclient.Config
	cache           cache.Cache
	eventRecorder   record.EventRecorder
	informerFactory informers.SharedInformerFactory
	runAllFilters   bool
	parallelizer    parallelize.Parallelizer
	metricsRecorder *metricsRecorder

	percentageOfClustersToTolerate int32
}

// Option for the frameworkImpl.
type Option func(*frameworkOptions)

// WithClientSet sets clientSet for the scheduling frameworkImpl.
func WithClientSet(clientSet clusternet.Interface) Option {
	return func(o *frameworkOptions) {
		o.clientSet = clientSet
	}
}

// WithKubeConfig sets kubeConfig for the scheduling frameworkImpl.
func WithKubeConfig(kubeConfig *restclient.Config) Option {
	return func(o *frameworkOptions) {
		o.kubeConfig = kubeConfig
	}
}

// WithEventRecorder sets clientSet for the scheduling frameworkImpl.
func WithEventRecorder(recorder record.EventRecorder) Option {
	return func(o *frameworkOptions) {
		o.eventRecorder = recorder
	}
}

// WithInformerFactory sets informer factory for the scheduling frameworkImpl.
func WithInformerFactory(informerFactory informers.SharedInformerFactory) Option {
	return func(o *frameworkOptions) {
		o.informerFactory = informerFactory
	}
}

// WithCache sets scheduler cache for the scheduling frameworkImpl.
func WithCache(cache cache.Cache) Option {
	return func(o *frameworkOptions) {
		o.cache = cache
	}
}

// WithRunAllFilters sets the runAllFilters flag, which means RunFilterPlugins accumulates
// all failure Statuses.
func WithRunAllFilters(runAllFilters bool) Option {
	return func(o *frameworkOptions) {
		o.runAllFilters = runAllFilters
	}
}

// WithParallelism sets parallelism for the scheduling frameworkImpl.
func WithParallelism(parallelism int) Option {
	return func(o *frameworkOptions) {
		o.parallelizer = parallelize.NewParallelizer(parallelism)
	}
}

// WithPercentageOfClustersToTolerate sets percentage of clusters to be tolerated for predicting failures.
func WithPercentageOfClustersToTolerate(percentageOfClustersToTolerate int32) Option {
	return func(o *frameworkOptions) {
		o.percentageOfClustersToTolerate = percentageOfClustersToTolerate
	}
}

func defaultFrameworkOptions() frameworkOptions {
	return frameworkOptions{
		metricsRecorder:                newMetricsRecorder(1000, time.Second),
		parallelizer:                   parallelize.NewParallelizer(parallelize.DefaultParallelism),
		percentageOfClustersToTolerate: schedulerapis.DefaultPercentageOfClustersToTolerate,
	}
}

var _ framework.Framework = &frameworkImpl{}

// NewFramework initializes plugins given the configuration and the registry.
func NewFramework(r Registry, profile *schedulerapis.SchedulerProfile, opts ...Option) (framework.Framework, error) {
	options := defaultFrameworkOptions()
	for _, opt := range opts {
		opt(&options)
	}

	f := &frameworkImpl{
		scorePluginWeight:              make(map[string]int),
		waitingSubscriptions:           newWaitingSubscriptionsMap(),
		clientSet:                      options.clientSet,
		kubeConfig:                     options.kubeConfig,
		eventRecorder:                  options.eventRecorder,
		informerFactory:                options.informerFactory,
		metricsRecorder:                options.metricsRecorder,
		runAllFilters:                  options.runAllFilters,
		parallelizer:                   options.parallelizer,
		profileName:                    "default",
		cache:                          options.cache,
		percentageOfClustersToTolerate: options.percentageOfClustersToTolerate,
	}

	if r == nil {
		return f, nil
	}
	if profile == nil {
		return f, nil
	}

	f.profileName = profile.SchedulerName
	if profile.Plugins == nil {
		return f, nil
	}
	// get needed plugins from config
	pg := f.pluginsNeeded(profile.Plugins)

	pluginConfig := make(map[string]runtime.Object, len(profile.PluginConfig))
	for i := range profile.PluginConfig {
		name := profile.PluginConfig[i].Name
		if _, ok := pluginConfig[name]; ok {
			return nil, fmt.Errorf("repeated config for plugin %s", name)
		}
		pluginConfig[name] = profile.PluginConfig[i].Args
	}

	// initialize plugins per individual extension points
	pluginsMap := make(map[string]framework.Plugin)
	for name, factory := range r {
		// initialize only needed plugins.
		if _, ok := pg[name]; !ok {
			continue
		}

		args := pluginConfig[name]
		p, err := factory(args, f)
		if err != nil {
			return nil, fmt.Errorf("initializing plugin %q: %v", name, err)
		}
		pluginsMap[name] = p
	}

	for _, e := range f.getExtensionPoints(profile.Plugins) {
		if err := updatePluginList(e.slicePtr, *e.plugins, pluginsMap); err != nil {
			return nil, err
		}
	}

	for _, scorePlugin := range profile.Plugins.Score.Enabled {
		// a weight of zero is not permitted, plugins can be disabled explicitly
		// when configured.
		if scorePlugin.Weight == 0 {
			return nil, fmt.Errorf("score plugin %q is not configured with weight", scorePlugin.Name)
		}
		f.scorePluginWeight[scorePlugin.Name] = int(scorePlugin.Weight)
	}

	if len(f.bindPlugins) == 0 {
		return nil, fmt.Errorf("at least one bind plugin is needed")
	}

	return f, nil
}

// copied from k8s.io/kubernetes/pkg/scheduler/framework/runtime/framework.go
func updatePluginList(pluginList interface{}, pluginSet schedulerapis.PluginSet, pluginsMap map[string]framework.Plugin) error {
	plugins := reflect.ValueOf(pluginList).Elem()
	pluginType := plugins.Type().Elem()
	set := sets.NewString()
	for _, ep := range pluginSet.Enabled {
		pg, ok := pluginsMap[ep.Name]
		if !ok {
			return fmt.Errorf("%s %q does not exist", pluginType.Name(), ep.Name)
		}

		if !reflect.TypeOf(pg).Implements(pluginType) {
			return fmt.Errorf("plugin %q does not extend %s plugin", ep.Name, pluginType.Name())
		}

		if set.Has(ep.Name) {
			return fmt.Errorf("plugin %q already registered as %q", ep.Name, pluginType.Name())
		}

		set.Insert(ep.Name)

		newPlugins := reflect.Append(plugins, reflect.ValueOf(pg))
		plugins.Set(newPlugins)
	}
	return nil
}

// RunPreFilterPlugins runs the set of configured PreFilter plugins. It returns
// *Status and its code is set to non-success if any of the plugins returns
// anything but Success. If a non-success status is returned, then the scheduling
// cycle is aborted.
func (f *frameworkImpl) RunPreFilterPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preFilter, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preFilterPlugins {
		status = f.runPreFilterPlugin(ctx, pl, state, sub)
		if !status.IsSuccess() {
			status.SetFailedPlugin(pl.Name())
			if status.IsUnschedulable() {
				return status
			}
			return framework.AsStatus(fmt.Errorf("running PreFilter plugin %q: %v", pl.Name(),
				status.AsError())).WithFailedPlugin(pl.Name())
		}
	}

	return nil
}

func (f *frameworkImpl) runPreFilterPlugin(ctx context.Context, pl framework.PreFilterPlugin, state *framework.CycleState, sub *appsapi.Subscription) *framework.Status {
	startTime := time.Now()
	status := pl.PreFilter(ctx, state, sub)
	f.metricsRecorder.observePluginDurationAsync(preFilter, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunFilterPlugins runs the set of configured Filter plugins for subscription on
// the given cluster. If any of these plugins doesn't return "Success", the
// given cluster is not suitable for running subscription.
// Meanwhile, the failure message and status are set for the given cluster.
func (f *frameworkImpl) RunFilterPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, cluster *clusterapi.ManagedCluster) framework.PluginToStatus {
	statuses := make(framework.PluginToStatus)
	for _, pl := range f.filterPlugins {
		pluginStatus := f.runFilterPlugin(ctx, pl, state, sub, cluster)
		if !pluginStatus.IsSuccess() {
			if !pluginStatus.IsUnschedulable() {
				// Filter plugins are not supposed to return any status other than
				// Success or Unschedulable.
				errStatus := framework.AsStatus(fmt.Errorf("running %q filter plugin: %v", pl.Name(),
					pluginStatus.AsError())).WithFailedPlugin(pl.Name())
				return map[string]*framework.Status{pl.Name(): errStatus}
			}
			pluginStatus.SetFailedPlugin(pl.Name())
			statuses[pl.Name()] = pluginStatus
			if !f.runAllFilters {
				// Exit early if we don't need to run all filters.
				return statuses
			}
		}
	}

	return statuses
}

func (f *frameworkImpl) runFilterPlugin(ctx context.Context, pl framework.FilterPlugin, state *framework.CycleState, sub *appsapi.Subscription, cluster *clusterapi.ManagedCluster) *framework.Status {
	startTime := time.Now()
	status := pl.Filter(ctx, state, sub, cluster)
	f.metricsRecorder.observePluginDurationAsync(Filter, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunPostFilterPlugins runs the set of configured PostFilter plugins until the first
// Success or Error is met, otherwise continues to execute all plugins.
func (f *frameworkImpl) RunPostFilterPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, filteredClusterStatusMap framework.ClusterToStatusMap) (_ *framework.PostFilterResult, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(postFilter, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()

	statuses := make(framework.PluginToStatus)
	for _, pl := range f.postFilterPlugins {
		r, s := f.runPostFilterPlugin(ctx, pl, state, sub, filteredClusterStatusMap)
		if s.IsSuccess() {
			return r, s
		} else if !s.IsUnschedulable() {
			// Any status other than Success or Unschedulable is Error.
			return nil, framework.AsStatus(s.AsError())
		}
		statuses[pl.Name()] = s
	}

	return nil, statuses.Merge()
}

func (f *frameworkImpl) runPostFilterPlugin(ctx context.Context, pl framework.PostFilterPlugin, state *framework.CycleState, sub *appsapi.Subscription, filteredClusterStatusMap framework.ClusterToStatusMap) (*framework.PostFilterResult, *framework.Status) {
	startTime := time.Now()
	r, s := pl.PostFilter(ctx, state, sub, filteredClusterStatusMap)
	f.metricsRecorder.observePluginDurationAsync(postFilter, pl.Name(), s, metrics.SinceInSeconds(startTime))
	return r, s
}

func (f *frameworkImpl) RunPrePredictPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, clusters []*clusterapi.ManagedCluster) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(prePredict, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.prePredictPlugins {
		status = f.runPrePredictPlugin(ctx, pl, state, sub, finv, clusters)
		if !status.IsSuccess() {
			return framework.AsStatus(fmt.Errorf("running PrePredict plugin %q: %v", pl.Name(), status.AsError()))
		}
	}

	return nil
}

func (f *frameworkImpl) runPrePredictPlugin(ctx context.Context, pl framework.PrePredictPlugin, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, clusters []*clusterapi.ManagedCluster) *framework.Status {
	startTime := time.Now()
	status := pl.PrePredict(ctx, state, sub, finv, clusters)
	f.metricsRecorder.observePluginDurationAsync(prePredict, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

func (f *frameworkImpl) RunPredictPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, clusters []*clusterapi.ManagedCluster, availableList framework.ClusterScoreList) (res framework.ClusterScoreList, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(predict, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	ctx, cancel := context.WithCancel(ctx)
	errCh := parallelize.NewErrorChannel()

	tolerations := int32(0)
	totalTolerations := int32(len(clusters)) * f.percentageOfClustersToTolerate
	tolerateFeasibleClusters := utilfeature.DefaultFeatureGate.Enabled(features.FeasibleClustersToleration)

	// Run Predict method for each cluster in parallel.
	f.Parallelizer().Until(ctx, len(clusters), func(index int) {
		for i, pl := range f.predictPlugins {
			replicas, status2 := f.runPredictPlugin(ctx, pl, state, sub, finv, clusters[index])
			if !status2.IsSuccess() {
				if tolerateFeasibleClusters {
					atomic.AddInt32(&tolerations, 1)
				}

				if !tolerateFeasibleClusters || tolerations > totalTolerations {
					err := fmt.Errorf("plugin %q failed with: %v", pl.Name(), status2.AsError())
					errCh.SendErrorWithCancel(err, cancel)
					return
				}

				// set nil FeedReplicas to this failed or unresponsive cluster
				replicas = make(framework.FeedReplicas, len(finv.Spec.Feeds))
			}
			if i == 0 {
				// First plugin, just set the replicas.
				availableList[index].MaxAvailableReplicas = replicas
			} else {
				// Get the minimum replicas from all predictors.
				availableList[index].MaxAvailableReplicas = mergeFeedReplicas(availableList[index].MaxAvailableReplicas, replicas)
			}
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("running Predict plugins: %v", err))
	}

	return availableList, nil
}

func (f *frameworkImpl) runPredictPlugin(ctx context.Context, pl framework.PredictPlugin, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, cluster *clusterapi.ManagedCluster) (framework.FeedReplicas, *framework.Status) {
	startTime := time.Now()
	replicas, status := pl.Predict(ctx, state, sub, finv, cluster)
	f.metricsRecorder.observePluginDurationAsync(predict, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return replicas, status
}

// RunPreScorePlugins runs the set of configured pre-score plugins. If any
// of these plugins returns any status other than "Success", the given subscription is rejected.
func (f *frameworkImpl) RunPreScorePlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preScore, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preScorePlugins {
		status = f.runPreScorePlugin(ctx, pl, state, sub, clusters)
		if !status.IsSuccess() {
			return framework.AsStatus(fmt.Errorf("running PreScore plugin %q: %v", pl.Name(), status.AsError()))
		}
	}

	return nil
}

func (f *frameworkImpl) runPreScorePlugin(ctx context.Context, pl framework.PreScorePlugin, state *framework.CycleState, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster) *framework.Status {
	startTime := time.Now()
	status := pl.PreScore(ctx, state, sub, clusters)
	f.metricsRecorder.observePluginDurationAsync(preScore, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunScorePlugins runs the set of configured scoring plugins. It returns a list that
// stores for each scoring plugin name the corresponding  ClusterScoreList(s).
// It also returns *Status, which is set to non-success if any of the plugins returns
// a non-success status.
func (f *frameworkImpl) RunScorePlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster) (ps framework.PluginToClusterScores, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(score, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	pluginToClusterScores := make(framework.PluginToClusterScores, len(f.scorePlugins))
	for _, pl := range f.scorePlugins {
		pluginToClusterScores[pl.Name()] = make(framework.ClusterScoreList, len(clusters))
	}
	ctx, cancel := context.WithCancel(ctx)
	errCh := parallelize.NewErrorChannel()

	// Run Score method for each cluster in parallel.
	var s int64
	f.Parallelizer().Until(ctx, len(clusters), func(index int) {
		for _, pl := range f.scorePlugins {
			clusterNamespacedName := klog.KObj(clusters[index]).String()
			s, status = f.runScorePlugin(ctx, pl, state, sub, clusterNamespacedName)
			if !status.IsSuccess() {
				err := fmt.Errorf("plugin %q failed with: %v", pl.Name(), status.AsError())
				errCh.SendErrorWithCancel(err, cancel)
				return
			}
			pluginToClusterScores[pl.Name()][index] = framework.ClusterScore{
				NamespacedName: clusterNamespacedName,
				Score:          s,
			}
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("running Score plugins: %v", err))
	}

	// Run NormalizeScore method for each ScorePlugin in parallel.
	f.Parallelizer().Until(ctx, len(f.scorePlugins), func(index int) {
		pl := f.scorePlugins[index]
		ClusterScoreList := pluginToClusterScores[pl.Name()]
		if pl.ScoreExtensions() == nil {
			return
		}
		status = f.runScoreExtension(ctx, pl, state, sub, ClusterScoreList)
		if !status.IsSuccess() {
			err := fmt.Errorf("plugin %q failed with: %v", pl.Name(), status.AsError())
			errCh.SendErrorWithCancel(err, cancel)
			return
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("running Normalize on Score plugins: %v", err))
	}

	// Apply score defaultWeights for each ScorePlugin in parallel.
	f.Parallelizer().Until(ctx, len(f.scorePlugins), func(index int) {
		pl := f.scorePlugins[index]
		// Score plugins' weight has been checked when they are initialized.
		weight := f.scorePluginWeight[pl.Name()]
		ClusterScoreList := pluginToClusterScores[pl.Name()]

		for i, clusterScore := range ClusterScoreList {
			// return error if score plugin returns invalid score.
			if clusterScore.Score > framework.MaxClusterScore || clusterScore.Score < framework.MinClusterScore {
				err := fmt.Errorf("plugin %q returns an invalid score %v, it should in the range of [%v, %v] after normalizing", pl.Name(), clusterScore.Score, framework.MinClusterScore, framework.MaxClusterScore)
				errCh.SendErrorWithCancel(err, cancel)
				return
			}
			ClusterScoreList[i].Score = clusterScore.Score * int64(weight)
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("applying score defaultWeights on Score plugins: %v", err))
	}

	return pluginToClusterScores, nil
}

func (f *frameworkImpl) runScorePlugin(ctx context.Context, pl framework.ScorePlugin, state *framework.CycleState, sub *appsapi.Subscription, clusterNamespace string) (int64, *framework.Status) {
	startTime := time.Now()
	s, status := pl.Score(ctx, state, sub, clusterNamespace)
	f.metricsRecorder.observePluginDurationAsync(score, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return s, status
}

func (f *frameworkImpl) runScoreExtension(ctx context.Context, pl framework.ScorePlugin, state *framework.CycleState, sub *appsapi.Subscription, ClusterScoreList framework.ClusterScoreList) *framework.Status {
	startTime := time.Now()
	status := pl.ScoreExtensions().NormalizeScore(ctx, state, sub, ClusterScoreList)
	f.metricsRecorder.observePluginDurationAsync(scoreExtensionNormalize, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

func (f *frameworkImpl) RunPreAssignPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preAssign, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preAssignPlugins {
		status = f.runPreAssignPlugin(ctx, pl, state, sub, finv, availableReplicas)
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running PreAssign plugin", "plugin", pl.Name(), "sub", klog.KObj(sub))
			return framework.AsStatus(fmt.Errorf("running PreAssign plugin %q: %v", pl.Name(), err))
		}
	}
	return nil
}

func (f *frameworkImpl) runPreAssignPlugin(ctx context.Context, pl framework.PreAssignPlugin, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) *framework.Status {
	startTime := time.Now()
	status := pl.PreAssign(ctx, state, sub, finv, availableReplicas)
	f.metricsRecorder.observePluginDurationAsync(preAssign, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

func (f *frameworkImpl) RunAssignPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) (result framework.TargetClusters, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(assign, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	if len(f.assignPlugins) == 0 {
		return framework.TargetClusters{}, framework.NewStatus(framework.Skip, "empty assign plugins")
	}
	for _, ap := range f.assignPlugins {
		result, status = f.runAssignPlugin(ctx, ap, state, sub, finv, availableReplicas)
		if status != nil && status.Code() == framework.Skip {
			continue
		}
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running Assign plugin", "plugin", ap.Name(), "sub", klog.KObj(sub))
			return framework.TargetClusters{}, framework.AsStatus(fmt.Errorf("running Assign plugin %q: %v", ap.Name(), err))
		}
		return result, status
	}
	return result, status
}

func (f *frameworkImpl) runAssignPlugin(ctx context.Context, ap framework.AssignPlugin, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) (framework.TargetClusters, *framework.Status) {
	startTime := time.Now()
	result, status := ap.Assign(ctx, state, sub, finv, availableReplicas)
	f.metricsRecorder.observePluginDurationAsync(assign, ap.Name(), status, metrics.SinceInSeconds(startTime))
	return result, status
}

func (f *frameworkImpl) RunPostAssignPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(postAssign, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.postAssignPlugins {
		status = f.runPostAssignPlugin(ctx, pl, state, sub, finv, availableReplicas)
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running PostAssign plugin", "plugin", pl.Name(), "sub", klog.KObj(sub))
			return framework.AsStatus(fmt.Errorf("running PostAssign plugin %q: %v", pl.Name(), err))
		}
	}
	return nil
}

func (f *frameworkImpl) runPostAssignPlugin(ctx context.Context, pl framework.PostAssignPlugin, state *framework.CycleState, sub *appsapi.Subscription, finv *appsapi.FeedInventory, availableReplicas framework.TargetClusters) *framework.Status {
	startTime := time.Now()
	status := pl.PostAssign(ctx, state, sub, finv, availableReplicas)
	f.metricsRecorder.observePluginDurationAsync(postAssign, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunPreBindPlugins runs the set of configured prebind plugins. It returns a
// failure (bool) if any of the plugins returns an error. It also returns an
// error containing the rejection message or the error occurred in the plugin.
func (f *frameworkImpl) RunPreBindPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preBind, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preBindPlugins {
		status = f.runPreBindPlugin(ctx, pl, state, sub, targetClusters)
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running PreBind plugin", "plugin", pl.Name(), "sub", klog.KObj(sub))
			return framework.AsStatus(fmt.Errorf("running PreBind plugin %q: %v", pl.Name(), err))
		}
	}
	return nil
}

func (f *frameworkImpl) runPreBindPlugin(ctx context.Context, pl framework.PreBindPlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	startTime := time.Now()
	status := pl.PreBind(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(preBind, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunBindPlugins runs the set of configured bind plugins until one returns a non `Skip` status.
func (f *frameworkImpl) RunBindPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(bind, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	if len(f.bindPlugins) == 0 {
		return framework.NewStatus(framework.Skip, "")
	}
	for _, bp := range f.bindPlugins {
		status = f.runBindPlugin(ctx, bp, state, sub, targetClusters)
		if status != nil && status.Code() == framework.Skip {
			continue
		}
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running Bind plugin", "plugin", bp.Name(), "sub", klog.KObj(sub))
			return framework.AsStatus(fmt.Errorf("running Bind plugin %q: %v", bp.Name(), err))
		}
		return status
	}
	return status
}

func (f *frameworkImpl) runBindPlugin(ctx context.Context, bp framework.BindPlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	startTime := time.Now()
	status := bp.Bind(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(bind, bp.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunPostBindPlugins runs the set of configured postbind plugins.
func (f *frameworkImpl) RunPostBindPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(postBind, framework.Success.String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.postBindPlugins {
		f.runPostBindPlugin(ctx, pl, state, sub, targetClusters)
	}
}

func (f *frameworkImpl) runPostBindPlugin(ctx context.Context, pl framework.PostBindPlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) {
	startTime := time.Now()
	pl.PostBind(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(postBind, pl.Name(), nil, metrics.SinceInSeconds(startTime))
}

// RunReservePluginsReserve runs the Reserve method in the set of configured
// reserve plugins. If any of these plugins returns an error, it does not
// continue running the remaining ones and returns the error. In such a case,
// the subscription will not be scheduled and the caller will be expected to call
// RunReservePluginsUnreserve.
func (f *frameworkImpl) RunReservePluginsReserve(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(reserve, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.reservePlugins {
		status = f.runReservePluginReserve(ctx, pl, state, sub, targetClusters)
		if !status.IsSuccess() {
			err := status.AsError()
			klog.ErrorS(err, "Failed running Reserve plugin", "plugin", pl.Name(), "subscription", klog.KObj(sub))
			return framework.AsStatus(fmt.Errorf("running Reserve plugin %q: %v", pl.Name(), err))
		}
	}
	return nil
}

func (f *frameworkImpl) runReservePluginReserve(ctx context.Context, pl framework.ReservePlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) *framework.Status {
	startTime := time.Now()
	status := pl.Reserve(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(reserve, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunReservePluginsUnreserve runs the Unreserve method in the set of
// configured reserve plugins.
func (f *frameworkImpl) RunReservePluginsUnreserve(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(unreserve, framework.Success.String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	// Execute the Unreserve operation of each reserve plugin in the
	// *reverse* order in which the Reserve operation was executed.
	for i := len(f.reservePlugins) - 1; i >= 0; i-- {
		f.runReservePluginUnreserve(ctx, f.reservePlugins[i], state, sub, targetClusters)
	}
}

func (f *frameworkImpl) runReservePluginUnreserve(ctx context.Context, pl framework.ReservePlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) {
	startTime := time.Now()
	pl.Unreserve(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(unreserve, pl.Name(), nil, metrics.SinceInSeconds(startTime))
}

// RunPermitPlugins runs the set of configured permit plugins. If any of these
// plugins returns a status other than "Success" or "Wait", it does not continue
// running the remaining plugins and returns an error. Otherwise, if any of the
// plugins returns "Wait", then this function will create and add waiting subscription
// to a map of currently waiting subs and return status with "Wait" code.
// Subscription will remain waiting subscription for the minimum duration returned by the permit plugins.
func (f *frameworkImpl) RunPermitPlugins(ctx context.Context, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(permit, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	pluginsWaitTime := make(map[string]time.Duration)
	statusCode := framework.Success
	var timeout time.Duration
	for _, pl := range f.permitPlugins {
		status, timeout = f.runPermitPlugin(ctx, pl, state, sub, targetClusters)
		if !status.IsSuccess() {
			if status.IsUnschedulable() {
				msg := fmt.Sprintf("rejected subscription %q by permit plugin %q: %v", sub.Name, pl.Name(), status.Message())
				klog.V(4).Infof(msg)
				status.SetFailedPlugin(pl.Name())
				return status
			}
			if status.Code() == framework.Wait {
				// Not allowed to be greater than maxTimeout.
				if timeout > maxTimeout {
					timeout = maxTimeout
				}
				pluginsWaitTime[pl.Name()] = timeout
				statusCode = framework.Wait
			} else {
				err := status.AsError()
				klog.ErrorS(err, "Failed running Permit plugin", "plugin", pl.Name(), "subscription", klog.KObj(sub))
				return framework.AsStatus(fmt.Errorf("running Permit plugin %q: %v", pl.Name(), err)).WithFailedPlugin(pl.Name())
			}
		}
	}
	if statusCode == framework.Wait {
		f.waitingSubscriptions.add(newWaitingSubscription(sub, pluginsWaitTime))
		msg := fmt.Sprintf("one or more plugins asked to wait and no plugin rejected subscription %q", sub.Name)
		klog.V(4).Infof(msg)
		return framework.NewStatus(framework.Wait, msg)
	}
	return nil
}

func (f *frameworkImpl) runPermitPlugin(ctx context.Context, pl framework.PermitPlugin, state *framework.CycleState, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (*framework.Status, time.Duration) {
	startTime := time.Now()
	status, timeout := pl.Permit(ctx, state, sub, targetClusters)
	f.metricsRecorder.observePluginDurationAsync(permit, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status, timeout
}

// WaitOnPermit will block, if the subscription is a waiting subscription, until the waiting subscription is rejected or allowed.
func (f *frameworkImpl) WaitOnPermit(ctx context.Context, sub *appsapi.Subscription) *framework.Status {
	waitingSubscription := f.waitingSubscriptions.get(sub.UID)
	if waitingSubscription == nil {
		return nil
	}
	defer f.waitingSubscriptions.remove(sub.UID)
	klog.V(4).Infof("subscription %q waiting on permit", sub.Name)

	startTime := time.Now()
	s := <-waitingSubscription.s
	metrics.PermitWaitDuration.WithLabelValues(s.Code().String()).Observe(metrics.SinceInSeconds(startTime))

	if !s.IsSuccess() {
		if s.IsUnschedulable() {
			msg := fmt.Sprintf("subscription %q rejected while waiting on permit: %v", sub.Name, s.Message())
			klog.V(4).Infof(msg)
			s.SetFailedPlugin(s.FailedPlugin())
			return s
		}
		err := s.AsError()
		klog.ErrorS(err, "Failed waiting on permit for subscription", "subscription", klog.KObj(sub))
		return framework.AsStatus(fmt.Errorf("waiting on permit for subscription: %v", err)).WithFailedPlugin(s.FailedPlugin())
	}
	return nil
}

// IterateOverWaitingSubscriptions acquires a read lock and iterates over the WaitingSubscriptions map.
func (f *frameworkImpl) IterateOverWaitingSubscriptions(callback func(framework.WaitingSubscription)) {
	f.waitingSubscriptions.iterate(callback)
}

// GetWaitingSubscription returns a reference to a WaitingSubscription given its UID.
func (f *frameworkImpl) GetWaitingSubscription(uid types.UID) framework.WaitingSubscription {
	if wp := f.waitingSubscriptions.get(uid); wp != nil {
		return wp
	}
	return nil // Returning nil instead of *waitingSubscription(nil).
}

// RejectWaitingSubscription rejects a WaitingSubscription given its UID.
func (f *frameworkImpl) RejectWaitingSubscription(uid types.UID) {
	waitingSubscription := f.waitingSubscriptions.get(uid)
	if waitingSubscription != nil {
		waitingSubscription.Reject("", "removed")
	}
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *frameworkImpl) HasFilterPlugins() bool {
	return len(f.filterPlugins) > 0
}

// HasPostFilterPlugins returns true if at least one postFilter plugin is defined.
func (f *frameworkImpl) HasPostFilterPlugins() bool {
	return len(f.postFilterPlugins) > 0
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *frameworkImpl) HasScorePlugins() bool {
	return len(f.scorePlugins) > 0
}

// HasPredictPlugins returns true if at least one predict plugin is defined.
func (f *frameworkImpl) HasPredictPlugins() bool {
	return len(f.predictPlugins) > 0
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins where configured.
func (f *frameworkImpl) ListPlugins() *schedulerapis.Plugins {
	m := schedulerapis.Plugins{}

	for _, e := range f.getExtensionPoints(&m) {
		plugins := reflect.ValueOf(e.slicePtr).Elem()
		extName := plugins.Type().Elem().Name()
		var cfgs []schedulerapis.Plugin
		for i := 0; i < plugins.Len(); i++ {
			name := plugins.Index(i).Interface().(framework.Plugin).Name()
			p := schedulerapis.Plugin{Name: name}
			if extName == "ScorePlugin" {
				// Weights apply only to score plugins.
				p.Weight = int32(f.scorePluginWeight[name])
			}
			cfgs = append(cfgs, p)
		}
		if len(cfgs) > 0 {
			e.plugins.Enabled = cfgs
		}
	}
	return &m
}

// ClientSet returns a clusternet clientset.
func (f *frameworkImpl) ClientSet() clusternet.Interface {
	return f.clientSet
}

// KubeConfig returns a kubeconfig.
func (f *frameworkImpl) KubeConfig() *restclient.Config {
	return f.kubeConfig
}

// EventRecorder returns an event recorder.
func (f *frameworkImpl) EventRecorder() record.EventRecorder {
	return f.eventRecorder
}

// SharedInformerFactory returns a shared informer factory.
func (f *frameworkImpl) SharedInformerFactory() informers.SharedInformerFactory {
	return f.informerFactory
}

// ProfileName returns the profile name associated to this framework.
func (f *frameworkImpl) ProfileName() string {
	return f.profileName
}

// Parallelizer returns a parallelizer holding parallelism for scheduler.
func (f *frameworkImpl) Parallelizer() parallelize.Parallelizer {
	return f.parallelizer
}

// ClusterCache returns a cluster cache.
func (f *frameworkImpl) ClusterCache() cache.Cache {
	return f.cache
}

func (f *frameworkImpl) pluginsNeeded(plugins *schedulerapis.Plugins) map[string]schedulerapis.Plugin {
	pgMap := make(map[string]schedulerapis.Plugin)

	if plugins == nil {
		return pgMap
	}

	find := func(pgs *schedulerapis.PluginSet) {
		for _, pg := range pgs.Enabled {
			pgMap[pg.Name] = pg
		}
	}
	for _, e := range f.getExtensionPoints(plugins) {
		find(e.plugins)
	}
	return pgMap
}

func mergeFeedReplicas(a, b framework.FeedReplicas) framework.FeedReplicas {
	if len(a) < len(b) {
		a, b = b, a
	}
	res := make(framework.FeedReplicas, len(a))
	for i := 0; i < len(b); i++ {
		if a[i] != nil && b[i] != nil {
			res[i] = utilpointer.Int32(utils.MinInt32(*a[i], *b[i]))
			continue
		}

		res[i] = nil
	}
	for i := len(b); i < len(a); i++ {
		res[i] = a[i]
	}
	return res
}
