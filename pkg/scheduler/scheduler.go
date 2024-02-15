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

package scheduler

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	apiserver "k8s.io/apiserver/pkg/server"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	controllermanagerapp "k8s.io/controller-manager/app"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/algorithm"
	schedulerapis "github.com/clusternet/clusternet/pkg/scheduler/apis"
	schedulercache "github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/scheduler/internal/queue"
	"github.com/clusternet/clusternet/pkg/scheduler/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/options"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
	"github.com/clusternet/clusternet/pkg/scheduler/profile"
	"github.com/clusternet/clusternet/pkg/utils"
)

// These are reasons for a subscription's transition to a condition.
const (
	// ReasonUnschedulable reason in SubscriptionScheduled SubscriptionCondition means that the scheduler
	// can't schedule the subscription right now, for example due to insufficient resources in the clusters.
	ReasonUnschedulable = "Unschedulable"

	// SchedulerError is the reason recorded for events when an error occurs during scheduling a subscription.
	SchedulerError = "SchedulerError"
)

// Scheduler defines configuration for clusternet scheduler
type Scheduler struct {
	schedulerOptions *options.SchedulerOptions

	SecureServing *apiserver.SecureServingInfo

	kubeClient                *kubernetes.Clientset
	electionClient            *kubernetes.Clientset
	clusternetClient          *clusternet.Clientset
	ClusternetInformerFactory informers.SharedInformerFactory

	subsLister applisters.SubscriptionLister
	subsSynced cache.InformerSynced

	inventoryLister applisters.FeedInventoryLister
	inventorySynced cache.InformerSynced

	// default in-tree registry
	registry frameworkruntime.Registry

	scheduleAlgorithm algorithm.ScheduleAlgorithm

	// SchedulingQueue holds subscriptions to be scheduled
	SchedulingQueue queue.SchedulingQueue

	// Profiles are the scheduling profiles.
	Profiles profile.Map

	lock           sync.RWMutex
	subscribersMap map[string][]appsapi.Subscriber
}

// NewScheduler returns a new Scheduler.
func NewScheduler(schedulerOptions *options.SchedulerOptions) (*Scheduler, error) {
	clientConfig, err := utils.LoadsKubeConfig(&schedulerOptions.ClientConnection)
	if err != nil {
		return nil, err
	}
	clientConfig.QPS = schedulerOptions.ClientConnection.QPS
	clientConfig.Burst = int(schedulerOptions.ClientConnection.Burst)

	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: clientConfig,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("kubeclient-clusternet-scheduler"))
	clusternetClient := clusternet.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-client-scheduler"))
	clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetClient, known.DefaultResync)

	var electionClient *kubernetes.Clientset
	if schedulerOptions.LeaderElection.LeaderElect {
		electionClient = kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-scheduler-election-client"))
	}

	// create event recorder
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-scheduler"})

	schedulerCache := schedulercache.New(clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister())
	percentageOfClustersToScore := schedulerapis.DefaultPercentageOfClustersToScore
	if schedulerOptions.SchedulerConfiguration != nil && schedulerOptions.SchedulerConfiguration.PercentageOfClustersToScore != nil {
		percentageOfClustersToScore = *schedulerOptions.SchedulerConfiguration.PercentageOfClustersToScore
	}

	// support out of tree plugins
	registry := plugins.NewInTreeRegistry()
	if err = registry.Merge(schedulerOptions.FrameworkOutOfTreeRegistry); err != nil {
		return nil, err
	}
	sched := &Scheduler{
		schedulerOptions:          schedulerOptions,
		kubeClient:                kubeClient,
		electionClient:            electionClient,
		clusternetClient:          clusternetClient,
		ClusternetInformerFactory: clusternetInformerFactory,
		subsLister:                clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		subsSynced:                clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().HasSynced,
		inventoryLister:           clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Lister(),
		inventorySynced:           clusternetInformerFactory.Apps().V1alpha1().FeedInventories().Informer().HasSynced,
		registry:                  registry,
		scheduleAlgorithm:         algorithm.NewGenericScheduler(schedulerCache, percentageOfClustersToScore),
		subscribersMap:            make(map[string][]appsapi.Subscriber),
		SchedulingQueue: queue.NewSchedulingQueue(
			framework.Less,
		),
	}

	var percentageOfClustersToTolerate int32
	var profiles []schedulerapis.SchedulerProfile
	if schedulerOptions.SchedulerConfiguration != nil {
		profiles = schedulerOptions.SchedulerConfiguration.Profiles
		if schedulerOptions.SchedulerConfiguration.PercentageOfClustersToTolerate != nil {
			percentageOfClustersToTolerate = *schedulerOptions.SchedulerConfiguration.PercentageOfClustersToTolerate
		}
	}
	//add default profile
	if len(profiles) == 0 {
		cfg := &schedulerapis.SchedulerConfiguration{}
		schedulerapis.SetDefaultsSchedulerConfiguration(cfg)
		if cfg.PercentageOfClustersToTolerate != nil {
			percentageOfClustersToTolerate = *cfg.PercentageOfClustersToTolerate
		}
		profiles = append([]schedulerapis.SchedulerProfile(nil), cfg.Profiles...)
	}
	profileMap, err := profile.NewMap(profiles, sched.registry,
		frameworkruntime.WithEventRecorder(recorder),
		frameworkruntime.WithInformerFactory(clusternetInformerFactory),
		frameworkruntime.WithCache(schedulerCache),
		frameworkruntime.WithClientSet(clusternetClient),
		frameworkruntime.WithKubeConfig(clientConfig),
		frameworkruntime.WithParallelism(parallelize.DefaultParallelism),
		frameworkruntime.WithRunAllFilters(false),
		frameworkruntime.WithPercentageOfClustersToTolerate(percentageOfClustersToTolerate),
	)
	if err != nil {
		return nil, err
	}
	sched.Profiles = profileMap

	// register all metrics
	metrics.Register()

	err = sched.addAllEventHandlers()
	return sched, err
}

// Run begins watching and scheduling. It starts scheduling and blocked until the context is done.
func (sched *Scheduler) Run(ctx context.Context) error {
	sched.SchedulingQueue.Run()
	err := sched.schedulerOptions.Config()
	if err != nil {
		return err
	}
	if err = sched.schedulerOptions.SecureServing.ApplyTo(&sched.SecureServing, nil); err != nil {
		return err
	}
	// Start up the metrics and healthz server.
	if sched.SecureServing != nil {
		handler := controllermanagerapp.BuildHandlerChain(
			utils.NewHealthzAndMetricsHandler("clusternet-scheduler", sched.schedulerOptions.DebuggingOptions),
			nil,
			nil,
		)
		if _, _, err = sched.SecureServing.Serve(handler, 0, ctx.Done()); err != nil {
			// fail early for secure handlers, removing the old error loop from above
			return fmt.Errorf("failed to start secure server: %v", err)
		}
	}

	defer sched.SchedulingQueue.Close()

	// Start all informers.
	sched.ClusternetInformerFactory.Start(ctx.Done())

	// Wait for all caches to sync before scheduling.
	sched.ClusternetInformerFactory.WaitForCacheSync(ctx.Done())

	// if leader election is disabled, so runCommand inline until done.
	if !sched.schedulerOptions.LeaderElection.LeaderElect {
		wait.UntilWithContext(ctx, sched.scheduleOne, 0)
		klog.Warning("finished without leader elect")
		return nil
	}

	// leader election is enabled, runCommand via LeaderElector until done and exit.
	curIdentity, err := utils.GenerateIdentity()
	if err != nil {
		return err
	}
	le, err := leaderelection.NewLeaderElector(*utils.NewLeaderElectionConfigWithDefaultValue(
		curIdentity,
		sched.schedulerOptions.LeaderElection.ResourceName,
		sched.schedulerOptions.LeaderElection.ResourceNamespace,
		sched.schedulerOptions.LeaderElection.LeaseDuration.Duration,
		sched.schedulerOptions.LeaderElection.RenewDeadline.Duration,
		sched.schedulerOptions.LeaderElection.RetryPeriod.Duration,
		sched.electionClient,
		leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				wait.UntilWithContext(ctx, sched.scheduleOne, 0)
			},
			OnStoppedLeading: func() {
				klog.Error("leader election got lost")
			},
			OnNewLeader: func(identity string) {
				// we're notified when new leader elected
				if identity == curIdentity {
					// I just got the lock
					return
				}
				klog.Infof("new leader elected: %s", identity)
			},
		},
	))
	if err != nil {
		return err
	}
	le.Run(ctx)
	return nil
}

// scheduleOne does the entire scheduling workflow for a single subscription.
// It is serialized on the scheduling algorithm's cluster fitting.
func (sched *Scheduler) scheduleOne(ctx context.Context) {
	subInfo, err := sched.SchedulingQueue.Pop()
	if err != nil {
		klog.Errorf("Error while retrieving next subscription from scheduling queue: %v", err)
		return
	}
	ns := subInfo.Subscription.Namespace
	name := subInfo.Subscription.Name
	klog.V(4).Infof("About to try and schedule subscription %v/%v", ns, name)

	sub, err := sched.subsLister.Subscriptions(ns).Get(name)
	if err != nil {
		if !errors.IsNotFound(err) {
			utilruntime.HandleError(err)
			if aerr := sched.SchedulingQueue.AddUnschedulableIfNotPresent(subInfo); aerr != nil {
				klog.ErrorS(aerr, "requeue subscription failed", "subscription", klog.KObj(subInfo.Subscription))
			}
		}
		return
	}
	subInfo.Subscription = sub
	klog.V(3).InfoS("Attempting to schedule subscription", "subscription", klog.KObj(sub))
	fwk, err := sched.frameworkForSubscription(sub)
	if err != nil {
		klog.ErrorS(err, "Unable to get profile", "subscription", klog.KObj(sub))
		return
	}
	var finv *appsapi.FeedInventory
	if sub.Spec.SchedulingStrategy == appsapi.DividingSchedulingStrategyType {
		finv, err = sched.inventoryLister.FeedInventories(ns).Get(name)
		if err != nil {
			if !errors.IsNotFound(err) {
				utilruntime.HandleError(err)
				if aerr := sched.SchedulingQueue.AddUnschedulableIfNotPresent(subInfo); aerr != nil {
					klog.ErrorS(aerr, "requeue subscription failed", "subscription", klog.KObj(subInfo.Subscription))
				}
			}
			return
		}
		feeds := make([]appsapi.Feed, 0, len(finv.Spec.Feeds))
		for i := range finv.Spec.Feeds {
			feeds = append(feeds, finv.Spec.Feeds[i].Feed)
		}
		if !reflect.DeepEqual(sub.Spec.Feeds, feeds) {
			return
		}
	}

	if !admit(sub, finv) {
		return
	}

	// Synchronously attempt to find a fit for the subscription.
	start := time.Now()
	state := framework.NewCycleState()

	schedulingCycleCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	scheduleResult, err := sched.scheduleAlgorithm.Schedule(schedulingCycleCtx, fwk, state, sub, finv)
	if err != nil {
		sched.handleSchedulingFailure(fwk, subInfo, err, ReasonUnschedulable)
		if !strings.Contains(err.Error(), "clusters are available") {
			return
		}
	}
	metrics.SchedulingAlgorithmLatency.Observe(metrics.SinceInSeconds(start))

	// Run the Reserve method of reserve plugins.
	targetClusters := scheduleResult.SuggestedClusters
	if sts := fwk.RunReservePluginsReserve(schedulingCycleCtx, state, sub, targetClusters); !sts.IsSuccess() {
		metrics.SubscriptionScheduleError(fwk.ProfileName(), metrics.SinceInSeconds(start))
		// trigger un-reserve to clean up state associated with the reserved subscription
		fwk.RunReservePluginsUnreserve(schedulingCycleCtx, state, sub, targetClusters)
		sched.handleSchedulingFailure(fwk, subInfo, sts.AsError(), SchedulerError)
		return
	}

	// Run "permit" plugins.
	runPermitStatus := fwk.RunPermitPlugins(schedulingCycleCtx, state, sub, targetClusters)
	if runPermitStatus.Code() != framework.Wait && !runPermitStatus.IsSuccess() {
		var reason string
		if runPermitStatus.IsUnschedulable() {
			metrics.SubscriptionUnschedulable(fwk.ProfileName(), metrics.SinceInSeconds(start))
			reason = ReasonUnschedulable
		} else {
			metrics.SubscriptionScheduleError(fwk.ProfileName(), metrics.SinceInSeconds(start))
			reason = SchedulerError
		}
		// One of the plugins returned status different from success or wait.
		fwk.RunReservePluginsUnreserve(schedulingCycleCtx, state, sub, targetClusters)
		sched.handleSchedulingFailure(fwk, subInfo, runPermitStatus.AsError(), reason)
		return
	}

	// bind the subscription to multiple clusters asynchronously (we can do this b/c of the assumption step above).
	go func() {
		bindingCycleCtx, cancel2 := context.WithCancel(ctx)
		defer cancel2()
		metrics.SchedulerGoroutines.WithLabelValues(metrics.Binding).Inc()
		defer metrics.SchedulerGoroutines.WithLabelValues(metrics.Binding).Dec()

		waitOnPermitStatus := fwk.WaitOnPermit(bindingCycleCtx, sub)
		if !waitOnPermitStatus.IsSuccess() {
			var reason string
			if waitOnPermitStatus.IsUnschedulable() {
				metrics.SubscriptionUnschedulable(fwk.ProfileName(), metrics.SinceInSeconds(start))
				reason = ReasonUnschedulable
			} else {
				metrics.SubscriptionScheduleError(fwk.ProfileName(), metrics.SinceInSeconds(start))
				reason = SchedulerError
			}
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			fwk.RunReservePluginsUnreserve(bindingCycleCtx, state, sub, targetClusters)
			sched.handleSchedulingFailure(fwk, subInfo, waitOnPermitStatus.AsError(), reason)
			return
		}

		// Run "prebind" plugins.
		preBindStatus := fwk.RunPreBindPlugins(bindingCycleCtx, state, sub, targetClusters)
		if !preBindStatus.IsSuccess() {
			metrics.SubscriptionScheduleError(fwk.ProfileName(), metrics.SinceInSeconds(start))
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			fwk.RunReservePluginsUnreserve(bindingCycleCtx, state, sub, targetClusters)
			sched.handleSchedulingFailure(fwk, subInfo, preBindStatus.AsError(), SchedulerError)
			return
		}

		err = sched.bind(bindingCycleCtx, state, fwk, sub, targetClusters)
		if err != nil {
			metrics.SubscriptionScheduleError(fwk.ProfileName(), metrics.SinceInSeconds(start))
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			fwk.RunReservePluginsUnreserve(bindingCycleCtx, state, sub, targetClusters)
			sched.handleSchedulingFailure(fwk, subInfo, fmt.Errorf("binding rejected: %v", err), SchedulerError)
		} else {
			metrics.SubscriptionScheduled(fwk.ProfileName(), metrics.SinceInSeconds(start))

			// Run "postbind" plugins.
			fwk.RunPostBindPlugins(bindingCycleCtx, state, sub, targetClusters)
		}
	}()
}

// bind a subscription to given clusters.
// We expect this to run asynchronously, so we handle binding metrics internally.
func (sched *Scheduler) bind(ctx context.Context, state *framework.CycleState, fwk framework.Framework, sub *appsapi.Subscription, targetClusters framework.TargetClusters) (err error) {
	defer func() {
		// finish binding
		if err != nil {
			klog.V(1).InfoS("Failed to bind sub", "sub", klog.KObj(sub))
			return
		}
		fwk.EventRecorder().Eventf(
			sub,
			corev1.EventTypeNormal,
			"Scheduled",
			"Successfully bound %s to %s",
			klog.KObj(sub), strings.Join(targetClusters.BindingClusters, ","),
		)
	}()

	bindStatus := fwk.RunBindPlugins(ctx, state, sub, targetClusters)
	if bindStatus.IsSuccess() {
		return nil
	}
	if bindStatus.Code() == framework.Error {
		return bindStatus.AsError()
	}
	return fmt.Errorf("bind status: %s, %v", bindStatus.Code().String(), bindStatus.Message())
}

// handleSchedulingFailure records an event for the subscription that indicates the
// subscription has failed to schedule. Also, update the subscription condition.
func (sched *Scheduler) handleSchedulingFailure(fwk framework.Framework, sInfo *framework.SubscriptionInfo, err error, _ string) {
	sub := sInfo.Subscription
	klog.V(2).InfoS("Unable to schedule subscription; waiting", "subscription", klog.KObj(sub), "err", err)

	msg := truncateMessage(err.Error())
	fwk.EventRecorder().Event(sub, corev1.EventTypeWarning, "FailedScheduling", msg)

	// TODO: update subscription condition

	// no need reschedule the subscription, when has no available cluster to schedule
	if strings.Contains(err.Error(), "clusters are available") {
		return
	}

	// re-added to the queue for re-processing
	if aerr := sched.SchedulingQueue.AddUnschedulableIfNotPresent(sInfo); aerr != nil {
		klog.ErrorS(aerr, "requeue subscription failed", "subscription", klog.KObj(sInfo.Subscription))
	}
}

// addAllEventHandlers is a helper function used in tests and in Scheduler
// to add event handlers for various informers.
func (sched *Scheduler) addAllEventHandlers() error {
	errors := []error{}

	_, err := sched.ClusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().AddEventHandler(cache.
		FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *appsapi.Subscription:
				sub := obj.(*appsapi.Subscription)
				if sub.DeletionTimestamp != nil {
					if err := sched.SchedulingQueue.Delete(sub); err != nil {
						utilruntime.HandleError(fmt.Errorf("unable to dequeue %T: %v", obj, err))
					}
					sched.lock.Lock()
					defer sched.lock.Unlock()
					delete(sched.subscribersMap, klog.KObj(sub).String())
					return false
				}

				return responsibleForSubscription(sub, sched.Profiles)
			case cache.DeletedFinalStateUnknown:
				if sub, ok := t.Obj.(*appsapi.Subscription); ok {
					return responsibleForSubscription(sub, sched.Profiles)
				}
				utilruntime.HandleError(fmt.Errorf("unable to convert object %T to *Subscription in %T", obj, sched))
				return false
			default:
				utilruntime.HandleError(fmt.Errorf("unable to handle object in %T: %T", sched, obj))
				return false
			}
		},
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				sub := obj.(*appsapi.Subscription)
				sched.lock.Lock()
				defer sched.lock.Unlock()
				sched.subscribersMap[klog.KObj(sub).String()] = sub.Spec.Subscribers
				if err := sched.SchedulingQueue.Add(sub); err != nil {
					klog.ErrorS(err, "add subscription to scheduling queue", "subscription", klog.KObj(sub))
				}
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldSub := oldObj.(*appsapi.Subscription)
				newSub := newObj.(*appsapi.Subscription)

				// Decide whether discovery has reported a spec change.
				if reflect.DeepEqual(oldSub.Spec, newSub.Spec) {
					klog.V(4).Infof("no updates on the spec of Subscription %s, skipping syncing", klog.KObj(oldSub))
					return
				}

				sched.lock.Lock()
				defer sched.lock.Unlock()
				sched.subscribersMap[klog.KObj(newSub).String()] = newSub.Spec.Subscribers
				if err := sched.SchedulingQueue.Add(newSub); err != nil {
					klog.ErrorS(err, "add subscription to scheduling queue", "subscription", klog.KObj(newSub))
				}
			},
		},
	})
	errors = append(errors, err)

	_, err = sched.ClusternetInformerFactory.Apps().V1alpha1().FeedInventories().Informer().AddEventHandler(cache.
		ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			finv := obj.(*appsapi.FeedInventory)
			sched.enqueueSubscriptionByKey(klog.KObj(finv).String())
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldInventory := oldObj.(*appsapi.FeedInventory)
			newInventory := newObj.(*appsapi.FeedInventory)
			if newInventory.ResourceVersion == oldInventory.ResourceVersion {
				// Periodic resync will send update events for all known Inventory.
				return
			}
			sched.enqueueSubscriptionByKey(klog.KObj(newInventory).String())
		},
	})
	errors = append(errors, err)

	enqueueSubscriptionForClusterFunc := func(newMcls *clusterapi.ManagedCluster, oldMcls *clusterapi.ManagedCluster) {
		sched.lock.RLock()
		defer sched.lock.RUnlock()

		for key, subscribers := range sched.subscribersMap {
			for _, subscriber := range subscribers {
				selector, err2 := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
				if err2 != nil {
					klog.ErrorDepth(5, fmt.Sprintf("failed to parse labelSelector in Subscription %s: %v", key, err2))
					continue
				}
				if newMcls != nil && oldMcls == nil {
					// For AddFunc
					if !selector.Matches(labels.Set(newMcls.Labels)) {
						continue
					}
				} else if newMcls != nil && oldMcls != nil {
					// For UpdateFunc
					if !selector.Matches(labels.Set(newMcls.Labels)) && !selector.Matches(labels.Set(oldMcls.Labels)) {
						continue
					}
				} else if newMcls == nil && oldMcls != nil {
					// For DeleteFunc
					if !selector.Matches(labels.Set(oldMcls.Labels)) {
						continue
					}
				}
				sched.enqueueSubscriptionByKey(key)
				break
			}
		}
	}

	_, err = sched.ClusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().AddEventHandler(cache.
		ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			mcls := obj.(*clusterapi.ManagedCluster)
			if mcls.DeletionTimestamp != nil {
				return
			}
			enqueueSubscriptionForClusterFunc(mcls, nil)
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldMcls := oldObj.(*clusterapi.ManagedCluster)
			newMcls := newObj.(*clusterapi.ManagedCluster)

			if newMcls.DeletionTimestamp != nil {
				return
			}

			if !utils.ClusterHasReadyCondition(newMcls) {
				if utilfeature.DefaultFeatureGate.Enabled(features.FailOver) {
					klog.V(4).Infof(
						"ManagedCluster %s is becoming not ready. Will fail over workloads to other spare clusters.",
						klog.KObj(newMcls),
					)
					enqueueSubscriptionForClusterFunc(nil, oldMcls)
					return
				}

				klog.WarningfDepth(4,
					"Can not fail over workloads running in not ready ManagedCluster %s, "+
						"due to disabled feature gate %s.",
					klog.KObj(newMcls), features.FailOver,
				)
			}

			// no updates on the labels/taints of ManagedCluster
			if reflect.DeepEqual(oldMcls.Labels, newMcls.Labels) && reflect.DeepEqual(oldMcls.Spec.Taints, newMcls.Spec.Taints) {
				klog.V(4).Infof("no updates on the labels/taints of ManagedCluster %s, skipping syncing", klog.KObj(oldMcls))
				return
			}
			enqueueSubscriptionForClusterFunc(newMcls, oldMcls)
		},
		DeleteFunc: func(obj interface{}) {
			// when a ManagedCluster is deleted,
			// - Auto populated objects, like Base and Description, will be auto-deleted on next sync/resync of subscribed Subscriptions
			// - If current dedicated namespace is deleted, then all objects in this namespaces will be pruned.
			mcls := obj.(*clusterapi.ManagedCluster)
			enqueueSubscriptionForClusterFunc(nil, mcls)
		},
	})
	errors = append(errors, err)

	return utilerrors.NewAggregate(errors)
}

func (sched *Scheduler) enqueueSubscriptionByKey(key string) {
	ns, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return
	}
	sub, err := sched.subsLister.Subscriptions(ns).Get(name)
	if err != nil {
		if !errors.IsNotFound(err) {
			utilruntime.HandleError(err)
		}
		return
	}
	if aerr := sched.SchedulingQueue.Add(sub); aerr != nil {
		klog.ErrorS(aerr, "add subscription to scheduling queue", "subscription", klog.KObj(sub))
	}
}

func (sched *Scheduler) frameworkForSubscription(sub *appsapi.Subscription) (framework.Framework, error) {
	fwk, ok := sched.Profiles[sub.Spec.SchedulerName]
	if !ok {
		return nil, fmt.Errorf("profile not found for scheduler name %q", sub.Spec.SchedulerName)
	}
	return fwk, nil
}

// responsibleForSubscription returns true if the subscription has asked to be scheduled by the given scheduler.
func responsibleForSubscription(subscription *appsapi.Subscription, profiles profile.Map) bool {
	return profiles.HandlesSchedulerName(subscription.Spec.SchedulerName)
}

// truncateMessage truncates a message if it hits the NoteLengthLimit.
// copied from k8s.io/kubernetes/pkg/scheduler/scheduler.go
func truncateMessage(message string) string {
	max := known.NoteLengthLimit
	if len(message) <= max {
		return message
	}
	suffix := " ..."
	return message[:max-len(suffix)] + suffix
}

func admit(sub *appsapi.Subscription, finv *appsapi.FeedInventory) bool {
	// always schedule replication subscription
	if sub.Spec.SchedulingStrategy == appsapi.ReplicaSchedulingStrategyType {
		return true
	}
	specHashChanged := utils.HashSubscriptionSpec(&sub.Spec) != sub.Status.SpecHash
	feedChanged := isFeedChanged(sub, finv)
	return specHashChanged || feedChanged
}

func isFeedChanged(sub *appsapi.Subscription, finv *appsapi.FeedInventory) bool {
	feeds := sub.Spec.Feeds
	replicas := sub.Status.Replicas
	if len(feeds) != len(replicas) {
		return true
	}
	for i := range feeds {
		if _, exist := replicas[utils.GetFeedKey(feeds[i])]; !exist {
			return true
		}
	}

	feedOrders := finv.Spec.Feeds
	for i := range feedOrders {
		var desired int32
		if feedOrders[i].DesiredReplicas != nil {
			desired = *feedOrders[i].DesiredReplicas
		}
		if utils.SumArrayInt32(replicas[utils.GetFeedKey(feedOrders[i].Feed)]) != desired {
			return true
		}
	}
	return false
}
