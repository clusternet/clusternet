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

package runtime

import (
	"context"
	"fmt"
	"reflect"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/informers"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	predictorapis "github.com/clusternet/clusternet/pkg/predictor/apis"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/predictor/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// Filter is the name of the filter extension point.
	Filter                  = "Filter"
	preFilter               = "PreFilter"
	postFilter              = "PostFilter"
	preCompute              = "PreCompute"
	compute                 = "Compute"
	preScore                = "PreScore"
	score                   = "Score"
	scoreExtensionNormalize = "ScoreExtensionNormalize"
	preAggregate            = "PreAggregate"
	aggregate               = "Aggregate"
)

// frameworkImpl is the component responsible for initializing and running predictor
// plugins.
type frameworkImpl struct {
	scorePluginWeight   map[string]int
	preFilterPlugins    []framework.PreFilterPlugin
	filterPlugins       []framework.FilterPlugin
	postFilterPlugins   []framework.PostFilterPlugin
	preComputePlugins   []framework.PreComputePlugin
	computePlugins      []framework.ComputePlugin
	preScorePlugins     []framework.PreScorePlugin
	scorePlugins        []framework.ScorePlugin
	preAggregatePlugins []framework.PreAggregatePlugin
	aggregatePlugins    []framework.AggregatePlugin

	clientSet       clientset.Interface
	kubeConfig      *restclient.Config
	eventRecorder   record.EventRecorder
	informerFactory informers.SharedInformerFactory

	metricsRecorder *metricsRecorder
	profileName     string

	parallelizer parallelize.Parallelizer

	// Indicates that RunFilterPlugins should accumulate all failed statuses and not return
	// after the first failure.
	runAllFilters bool
}

// extensionPoint encapsulates desired and applied set of plugins at a specific extension
// point. This is used to simplify iterating over all extension points supported by the
// frameworkImpl.
type extensionPoint struct {
	// the set of plugins to be configured at this extension point.
	plugins *predictorapis.PluginSet
	// a pointer to the slice storing plugins implementations that will run at this
	// extension point.
	slicePtr interface{}
}

func (f *frameworkImpl) getExtensionPoints(plugins *predictorapis.Plugins) []extensionPoint {
	return []extensionPoint{
		{&plugins.PreFilter, &f.preFilterPlugins},
		{&plugins.Filter, &f.filterPlugins},
		{&plugins.PostFilter, &f.postFilterPlugins},
		{&plugins.PreCompute, &f.preComputePlugins},
		{&plugins.Compute, &f.computePlugins},
		{&plugins.PreScore, &f.preScorePlugins},
		{&plugins.Score, &f.scorePlugins},
		{&plugins.PreAggregate, &f.preAggregatePlugins},
		{&plugins.Aggregate, &f.aggregatePlugins},
	}
}

type frameworkOptions struct {
	clientSet       clientset.Interface
	kubeConfig      *restclient.Config
	eventRecorder   record.EventRecorder
	informerFactory informers.SharedInformerFactory
	metricsRecorder *metricsRecorder
	runAllFilters   bool
	parallelizer    parallelize.Parallelizer
}

// Option for the frameworkImpl.
type Option func(*frameworkOptions)

// WithClientSet sets clientSet for the predictor frameworkImpl.
func WithClientSet(clientSet clientset.Interface) Option {
	return func(o *frameworkOptions) {
		o.clientSet = clientSet
	}
}

// WithKubeConfig sets kubeConfig for the predictor frameworkImpl.
func WithKubeConfig(kubeConfig *restclient.Config) Option {
	return func(o *frameworkOptions) {
		o.kubeConfig = kubeConfig
	}
}

// WithEventRecorder sets clientSet for the predictor frameworkImpl.
func WithEventRecorder(recorder record.EventRecorder) Option {
	return func(o *frameworkOptions) {
		o.eventRecorder = recorder
	}
}

// WithInformerFactory sets informer factory for the predictor frameworkImpl.
func WithInformerFactory(informerFactory informers.SharedInformerFactory) Option {
	return func(o *frameworkOptions) {
		o.informerFactory = informerFactory
	}
}

// WithRunAllFilters sets the runAllFilters flag, which means RunFilterPlugins accumulates
// all failure Statuses.
func WithRunAllFilters(runAllFilters bool) Option {
	return func(o *frameworkOptions) {
		o.runAllFilters = runAllFilters
	}
}

// WithParallelism sets parallelism for the predictor frameworkImpl.
func WithParallelism(parallelism int) Option {
	return func(o *frameworkOptions) {
		o.parallelizer = parallelize.NewParallelizer(parallelism)
	}
}

func defaultFrameworkOptions() frameworkOptions {
	return frameworkOptions{
		metricsRecorder: newMetricsRecorder(1000, time.Second),
		parallelizer:    parallelize.NewParallelizer(parallelize.DefaultParallelism),
	}
}

var _ framework.Framework = &frameworkImpl{}

// NewFramework initializes plugins given the configuration and the registry.
func NewFramework(r Registry, plugins *predictorapis.Plugins, opts ...Option) (framework.Framework, error) {
	options := defaultFrameworkOptions()
	for _, opt := range opts {
		opt(&options)
	}

	f := &frameworkImpl{
		scorePluginWeight: make(map[string]int),
		clientSet:         options.clientSet,
		kubeConfig:        options.kubeConfig,
		eventRecorder:     options.eventRecorder,
		informerFactory:   options.informerFactory,
		metricsRecorder:   options.metricsRecorder,
		runAllFilters:     options.runAllFilters,
		parallelizer:      options.parallelizer,
		profileName:       "default",
	}

	if r == nil {
		return f, nil
	}

	// initialize plugins per individual extension points
	pluginsMap := make(map[string]framework.Plugin)
	for name, factory := range r {
		p, err := factory(nil, f)
		if err != nil {
			return nil, fmt.Errorf("initializing plugin %q: %v", name, err)
		}
		pluginsMap[name] = p
	}

	for _, e := range f.getExtensionPoints(plugins) {
		if err := updatePluginList(e.slicePtr, *e.plugins, pluginsMap); err != nil {
			return nil, err
		}
	}

	for _, scorePlugin := range plugins.Score.Enabled {
		// a weight of zero is not permitted, plugins can be disabled explicitly
		// when configured.
		if scorePlugin.Weight == 0 {
			return nil, fmt.Errorf("score plugin %q is not configured with weight", scorePlugin.Name)
		}
		f.scorePluginWeight[scorePlugin.Name] = int(scorePlugin.Weight)
	}

	return f, nil
}

func updatePluginList(pluginList interface{}, pluginSet predictorapis.PluginSet, pluginsMap map[string]framework.Plugin) error {
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
// anything but Success. If a non-success status is returned, then the predicting
// cycle is aborted.
func (f *frameworkImpl) RunPreFilterPlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preFilter, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preFilterPlugins {
		status = f.runPreFilterPlugin(ctx, pl, requirements)
		if !status.IsSuccess() {
			status.SetFailedPlugin(pl.Name())
			if status.IsUnpredictable() {
				return status
			}
			return framework.AsStatus(fmt.Errorf("running PreFilter plugin %q: %v", pl.Name(),
				status.AsError())).WithFailedPlugin(pl.Name())
		}
	}

	return nil
}

func (f *frameworkImpl) runPreFilterPlugin(ctx context.Context, pl framework.PreFilterPlugin, requirements *appsapi.ReplicaRequirements) *framework.Status {
	startTime := time.Now()
	status := pl.PreFilter(ctx, requirements)
	f.metricsRecorder.observePluginDurationAsync(preFilter, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunFilterPlugins runs the set of configured Filter plugins for requirements on
// the given node. If any of these plugins doesn't return "Success", the
// given node is not suitable for running requirements.
// Meanwhile, the failure message and status are set for the given node.
func (f *frameworkImpl) RunFilterPlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodeInfo *framework.NodeInfo) framework.PluginToStatus {
	statuses := make(framework.PluginToStatus)
	for _, pl := range f.filterPlugins {
		pluginStatus := f.runFilterPlugin(ctx, pl, requirements, nodeInfo)
		if !pluginStatus.IsSuccess() {
			if !pluginStatus.IsUnpredictable() {
				// Filter plugins are not supposed to return any status other than
				// Success or Unpredictable.
				errStatus := framework.AsStatus(fmt.Errorf("running %q filter plugin: %v", pl.Name(),
					pluginStatus.AsError())).WithFailedPlugin(pl.Name())
				return map[string]*framework.Status{pl.Name(): errStatus}
			}
			pluginStatus.SetFailedPlugin(pl.Name())
			statuses[pl.Name()] = pluginStatus
		}
	}

	return statuses
}

func (f *frameworkImpl) runFilterPlugin(ctx context.Context, pl framework.FilterPlugin, requirements *appsapi.ReplicaRequirements, nodeInfo *framework.NodeInfo) *framework.Status {
	startTime := time.Now()
	status := pl.Filter(ctx, requirements, nodeInfo)
	f.metricsRecorder.observePluginDurationAsync(Filter, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunPostFilterPlugins runs the set of configured PostFilter plugins until the first
// Success or Error is met, otherwise continues to execute all plugins.
func (f *frameworkImpl) RunPostFilterPlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, filteredNodeStatusMap framework.NodeToStatusMap) (_ *framework.PostFilterResult, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(postFilter, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()

	statuses := make(framework.PluginToStatus)
	for _, pl := range f.postFilterPlugins {
		r, s := f.runPostFilterPlugin(ctx, pl, requirements, filteredNodeStatusMap)
		if s.IsSuccess() {
			return r, s
		} else if !s.IsUnpredictable() {
			// Any status other than Success or Unpredictable is Error.
			return nil, framework.AsStatus(s.AsError())
		}
		statuses[pl.Name()] = s
	}

	return nil, statuses.Merge()
}

func (f *frameworkImpl) runPostFilterPlugin(ctx context.Context, pl framework.PostFilterPlugin, requirements *appsapi.ReplicaRequirements, filteredNodeStatusMap framework.NodeToStatusMap) (*framework.PostFilterResult, *framework.Status) {
	startTime := time.Now()
	r, s := pl.PostFilter(ctx, requirements, filteredNodeStatusMap)
	f.metricsRecorder.observePluginDurationAsync(postFilter, pl.Name(), s, metrics.SinceInSeconds(startTime))
	return r, s
}

// RunPreComputePlugins runs the set of configured pre-compute plugins. If any
// of these plugins returns any status other than "Success", the given requirement is rejected.
func (f *frameworkImpl) RunPreComputePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodesInfo []*framework.NodeInfo) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preCompute, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preComputePlugins {
		status = f.runPreComputePlugin(ctx, pl, requirements, nodesInfo)
		if !status.IsSuccess() {
			return framework.AsStatus(fmt.Errorf("running PreCompute plugin %q: %v", pl.Name(), status.AsError()))
		}
	}

	return nil
}

func (f *frameworkImpl) runPreComputePlugin(ctx context.Context, pl framework.PreComputePlugin, requirements *appsapi.ReplicaRequirements, nodesInfo []*framework.NodeInfo) *framework.Status {
	startTime := time.Now()
	status := pl.PreCompute(ctx, requirements, nodesInfo)
	f.metricsRecorder.observePluginDurationAsync(preCompute, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunComputePlugins runs the set of configured compute plugins. It returns a list that
// stores for each Compute plugin name the corresponding NodeScoreList(s).
// It also returns *Status, which is set to non-success if any of the plugins returns
// a non-success status.
func (f *frameworkImpl) RunComputePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodesInfo []*framework.NodeInfo, availableList framework.NodeScoreList) (res framework.NodeScoreList, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(compute, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	ctx, cancel := context.WithCancel(ctx)
	errCh := parallelize.NewErrorChannel()

	// Run Compute method for each cluster in parallel.
	f.Parallelizer().Until(ctx, len(nodesInfo), func(index int) {
		for i, pl := range f.computePlugins {
			replicas, calStatus := f.runComputePlugin(ctx, pl, requirements, nodesInfo[index])
			if !calStatus.IsSuccess() {
				err := fmt.Errorf("plugin %q failed with: %v", pl.Name(), calStatus.AsError())
				errCh.SendErrorWithCancel(err, cancel)
				return
			}
			if i == 0 {
				// First plugin, just set the replicas.
				availableList[index].MaxAvailableReplicas = replicas
			} else {
				// Get the minimum replicas from all predictors.
				availableList[index].MaxAvailableReplicas = utils.MinInt32(availableList[index].MaxAvailableReplicas, replicas)
			}
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("running Compute plugins: %v", err))
	}

	return availableList, nil
}

func (f *frameworkImpl) runComputePlugin(ctx context.Context, pl framework.ComputePlugin, requirements *appsapi.ReplicaRequirements, nodeInfo *framework.NodeInfo) (int32, *framework.Status) {
	startTime := time.Now()
	replicas, status := pl.Compute(ctx, requirements, nodeInfo)
	f.metricsRecorder.observePluginDurationAsync(compute, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return replicas, status
}

// RunPreScorePlugins runs the set of configured pre-score plugins. If any
// of these plugins returns any status other than "Success", the given requirement is rejected.
func (f *frameworkImpl) RunPreScorePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodes []*v1.Node) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preScore, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preScorePlugins {
		status = f.runPreScorePlugin(ctx, pl, requirements, nodes)
		if !status.IsSuccess() {
			return framework.AsStatus(fmt.Errorf("running PreScore plugin %q: %v", pl.Name(), status.AsError()))
		}
	}

	return nil
}

func (f *frameworkImpl) runPreScorePlugin(ctx context.Context, pl framework.PreScorePlugin, requirements *appsapi.ReplicaRequirements, nodes []*v1.Node) *framework.Status {
	startTime := time.Now()
	status := pl.PreScore(ctx, requirements, nodes)
	f.metricsRecorder.observePluginDurationAsync(preScore, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

// RunScorePlugins runs the set of configured scoring plugins. It returns a list that
// stores for each scoring plugin name the corresponding NodeScoreList(s).
// It also returns *Status, which is set to non-success if any of the plugins returns
// a non-success status.
func (f *frameworkImpl) RunScorePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, nodes []*v1.Node) (ps framework.PluginToNodeScores, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(score, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	pluginToNodeScores := make(framework.PluginToNodeScores, len(f.scorePlugins))
	for _, pl := range f.scorePlugins {
		pluginToNodeScores[pl.Name()] = make(framework.NodeScoreList, len(nodes))
	}
	ctx, cancel := context.WithCancel(ctx)
	errCh := parallelize.NewErrorChannel()

	var s int64
	// Run Score method for each node in parallel.
	f.Parallelizer().Until(ctx, len(nodes), func(index int) {
		for _, pl := range f.scorePlugins {
			nodeName := nodes[index].Name
			s, status = f.runScorePlugin(ctx, pl, requirements, nodeName)
			if !status.IsSuccess() {
				err := fmt.Errorf("plugin %q failed with: %v", pl.Name(), status.AsError())
				errCh.SendErrorWithCancel(err, cancel)
				return
			}
			pluginToNodeScores[pl.Name()][index] = framework.NodeScore{
				Name:  nodeName,
				Score: s,
			}
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("running Score plugins: %v", err))
	}

	// Run NormalizeScore method for each ScorePlugin in parallel.
	f.Parallelizer().Until(ctx, len(f.scorePlugins), func(index int) {
		pl := f.scorePlugins[index]
		nodeScoreList := pluginToNodeScores[pl.Name()]
		if pl.ScoreExtensions() == nil {
			return
		}
		status = f.runScoreExtension(ctx, pl, requirements, nodeScoreList)
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
		nodeScoreList := pluginToNodeScores[pl.Name()]

		for i, nodeScore := range nodeScoreList {
			// return error if score plugin returns invalid score.
			if nodeScore.Score > framework.MaxNodeScore || nodeScore.Score < framework.MinNodeScore {
				err := fmt.Errorf("plugin %q returns an invalid score %v, it should in the range of [%v, %v] after normalizing", pl.Name(), nodeScore.Score, framework.MinNodeScore, framework.MaxNodeScore)
				errCh.SendErrorWithCancel(err, cancel)
				return
			}
			nodeScoreList[i].Score = nodeScore.Score * int64(weight)
		}
	})
	if err := errCh.ReceiveError(); err != nil {
		return nil, framework.AsStatus(fmt.Errorf("applying score defaultWeights on Score plugins: %v", err))
	}

	return pluginToNodeScores, nil
}

func (f *frameworkImpl) runScorePlugin(ctx context.Context, pl framework.ScorePlugin, requirements *appsapi.ReplicaRequirements, nodeName string) (int64, *framework.Status) {
	startTime := time.Now()
	s, status := pl.Score(ctx, requirements, nodeName)
	f.metricsRecorder.observePluginDurationAsync(score, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return s, status
}

func (f *frameworkImpl) runScoreExtension(ctx context.Context, pl framework.ScorePlugin, requirements *appsapi.ReplicaRequirements, nodeScoreList framework.NodeScoreList) *framework.Status {
	startTime := time.Now()
	status := pl.ScoreExtensions().NormalizeScore(ctx, requirements, nodeScoreList)
	f.metricsRecorder.observePluginDurationAsync(scoreExtensionNormalize, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

func (f *frameworkImpl) RunPreAggregatePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores framework.NodeScoreList) (status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(preAggregate, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	for _, pl := range f.preAggregatePlugins {
		status = f.runPreAggregatePlugin(ctx, pl, requirements, scores)
		if !status.IsSuccess() {
			return framework.AsStatus(fmt.Errorf("running PreAssign plugin %q: %v", pl.Name(), status.AsError()))
		}
	}
	return nil
}

func (f *frameworkImpl) runPreAggregatePlugin(ctx context.Context, pl framework.PreAggregatePlugin, requirements *appsapi.ReplicaRequirements, scores framework.NodeScoreList) *framework.Status {
	startTime := time.Now()
	status := pl.PreAggregate(ctx, requirements, scores)
	f.metricsRecorder.observePluginDurationAsync(preAggregate, pl.Name(), status, metrics.SinceInSeconds(startTime))
	return status
}

func (f *frameworkImpl) RunAggregatePlugins(ctx context.Context, requirements *appsapi.ReplicaRequirements, scores framework.NodeScoreList) (result schedulerapi.PredictorReplicas, status *framework.Status) {
	startTime := time.Now()
	defer func() {
		metrics.FrameworkExtensionPointDuration.WithLabelValues(aggregate, status.Code().String(), f.profileName).Observe(metrics.SinceInSeconds(startTime))
	}()
	if len(f.aggregatePlugins) == 0 {
		return nil, framework.NewStatus(framework.Skip, "empty aggregate plugins")
	}
	result = make(schedulerapi.PredictorReplicas)
	for _, ap := range f.aggregatePlugins {
		aggreateResult, pluginStatus := f.runAggregatePlugin(ctx, ap, requirements, scores)
		if pluginStatus != nil && pluginStatus.Code() == framework.Skip {
			continue
		}
		if !pluginStatus.IsSuccess() {
			return nil, framework.AsStatus(fmt.Errorf("running Assign plugin %q: %v", ap.Name(), status.AsError()))
		}
		for key := range aggreateResult {
			if _, ok := result[key]; ok {
				return nil, framework.AsStatus(fmt.Errorf("duplicate key %q in map created by Aggregate plugin %q", key, ap.Name()))
			}
			result[key] = aggreateResult[key]
		}
	}
	return result, nil
}

func (f *frameworkImpl) runAggregatePlugin(ctx context.Context, ap framework.AggregatePlugin, requirements *appsapi.ReplicaRequirements, scores framework.NodeScoreList) (result schedulerapi.PredictorReplicas, status *framework.Status) {
	startTime := time.Now()
	result, status = ap.Aggregate(ctx, requirements, scores)
	f.metricsRecorder.observePluginDurationAsync(aggregate, ap.Name(), status, metrics.SinceInSeconds(startTime))
	return result, status
}

// HasFilterPlugins returns true if at least one filter plugin is defined.
func (f *frameworkImpl) HasFilterPlugins() bool {
	return len(f.filterPlugins) > 0
}

// HasPostFilterPlugins returns true if at least one postFilter plugin is defined.
func (f *frameworkImpl) HasPostFilterPlugins() bool {
	return len(f.postFilterPlugins) > 0
}

// HasComputePlugins returns true if at least one compute plugin is defined.
func (f *frameworkImpl) HasComputePlugins() bool {
	return len(f.computePlugins) > 0
}

// HasScorePlugins returns true if at least one score plugin is defined.
func (f *frameworkImpl) HasScorePlugins() bool {
	return len(f.scorePlugins) > 0
}

// HasAggregatePlugins returns true if at least one aggregate plugin is defined.
func (f *frameworkImpl) HasAggregatePlugins() bool {
	return len(f.aggregatePlugins) > 0
}

// ListPlugins returns a map of extension point name to plugin names configured at each extension
// point. Returns nil if no plugins where configured.
func (f *frameworkImpl) ListPlugins() *predictorapis.Plugins {
	m := predictorapis.Plugins{}

	for _, e := range f.getExtensionPoints(&m) {
		plugins := reflect.ValueOf(e.slicePtr).Elem()
		extName := plugins.Type().Elem().Name()
		var enabledPlugins []predictorapis.Plugin
		for i := 0; i < plugins.Len(); i++ {
			name := plugins.Index(i).Interface().(framework.Plugin).Name()
			p := predictorapis.Plugin{Name: name}
			if extName == "ScorePlugin" {
				// Weights apply only to score plugins.
				p.Weight = int32(f.scorePluginWeight[name])
			}
			enabledPlugins = append(enabledPlugins, p)
		}
		if len(enabledPlugins) > 0 {
			e.plugins.Enabled = enabledPlugins
		}
	}
	return &m
}

// ClientSet returns a kubernetes clientset.
func (f *frameworkImpl) ClientSet() clientset.Interface {
	return f.clientSet
}

// KubeConfig returns a kubernetes config.
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

// Parallelizer returns a parallelizer holding parallelism for predictor.
func (f *frameworkImpl) Parallelizer() parallelize.Parallelizer {
	return f.parallelizer
}

// ProfileName returns the profile name associated to this framework.
func (f *frameworkImpl) ProfileName() string {
	return f.profileName
}
