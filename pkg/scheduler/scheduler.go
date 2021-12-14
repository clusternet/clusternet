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
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	clusternet "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	applisters "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/scheduler/algorithm"
	schedulercache "github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/plugins"
	frameworkruntime "github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/scheduler/metrics"
	"github.com/clusternet/clusternet/pkg/scheduler/options"
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

	kubeClient                *kubernetes.Clientset
	clusternetClient          *clusternet.Clientset
	ClusternetInformerFactory informers.SharedInformerFactory

	subsLister applisters.SubscriptionLister
	subsSynced cache.InformerSynced

	// default in-tree registry
	registry frameworkruntime.Registry

	scheduleAlgorithm algorithm.ScheduleAlgorithm

	// SchedulingQueue holds subscriptions to be scheduled
	SchedulingQueue workqueue.RateLimitingInterface

	framework framework.Framework
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

	// create event recorder
	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-scheduler"})

	schedulerCache := schedulercache.New(clusternetInformerFactory.Clusters().V1beta1().ManagedClusters().Lister())

	sched := &Scheduler{
		schedulerOptions:          schedulerOptions,
		kubeClient:                kubeClient,
		clusternetClient:          clusternetClient,
		ClusternetInformerFactory: clusternetInformerFactory,
		subsLister:                clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Lister(),
		subsSynced:                clusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().HasSynced,
		registry:                  plugins.NewInTreeRegistry(),
		scheduleAlgorithm:         algorithm.NewGenericScheduler(schedulerCache),
		SchedulingQueue:           workqueue.NewRateLimitingQueue(workqueue.DefaultItemBasedRateLimiter()),
	}

	framework, err := frameworkruntime.NewFramework(sched.registry,
		frameworkruntime.WithEventRecorder(recorder),
	)
	if err != nil {
		return nil, err
	}
	sched.framework = framework

	// register all metrics
	metrics.Register()

	addAllEventHandlers(sched)
	return sched, nil
}

// Run begins watching and scheduling. It starts scheduling and blocked until the context is done.
func (sched *Scheduler) Run(ctx context.Context) error {
	defer sched.SchedulingQueue.ShutDown()

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
		sched.kubeClient,
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
	key, shutdown := sched.SchedulingQueue.Get()
	if shutdown {
		klog.Error("failed to get next unscheduled subscription from closed queue")
		return
	}
	defer sched.SchedulingQueue.Done(key)

	// TODO: scheduling
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return
	}

	sub, err := sched.subsLister.Subscriptions(ns).Get(name)
	if err != nil {
		utilruntime.HandleError(err)
		return
	}
	klog.V(3).InfoS("Attempting to schedule subscription", "subscription", klog.KObj(sub))

	// Synchronously attempt to find a fit for the subscription.
	start := time.Now()

	schedulingCycleCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	scheduleResult, err := sched.scheduleAlgorithm.Schedule(schedulingCycleCtx, sched.framework, sub)
	if err != nil {
		sched.recordSchedulingFailure(sub, err, ReasonUnschedulable)
		return
	}
	metrics.SchedulingAlgorithmLatency.Observe(metrics.SinceInSeconds(start))

	// Run the Reserve method of reserve plugins.
	var namespacedClusters []string
	for _, cluster := range scheduleResult.SuggestedClusters {
		namespacedClusters = append(namespacedClusters, cluster)
	}
	if sts := sched.framework.RunReservePluginsReserve(schedulingCycleCtx, sub, namespacedClusters); !sts.IsSuccess() {
		metrics.SubscriptionScheduleError(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
		// trigger un-reserve to clean up state associated with the reserved subscription
		sched.framework.RunReservePluginsUnreserve(schedulingCycleCtx, sub, namespacedClusters)
		sched.recordSchedulingFailure(sub, sts.AsError(), SchedulerError)
		return
	}

	// Run "permit" plugins.
	runPermitStatus := sched.framework.RunPermitPlugins(schedulingCycleCtx, sub, namespacedClusters)
	if runPermitStatus.Code() != framework.Wait && !runPermitStatus.IsSuccess() {
		var reason string
		if runPermitStatus.IsUnschedulable() {
			metrics.SubscriptionUnschedulable(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
			reason = ReasonUnschedulable
		} else {
			metrics.SubscriptionScheduleError(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
			reason = SchedulerError
		}
		// One of the plugins returned status different from success or wait.
		sched.framework.RunReservePluginsUnreserve(schedulingCycleCtx, sub, namespacedClusters)
		sched.recordSchedulingFailure(sub, runPermitStatus.AsError(), reason)
		return
	}

	// bind the subscription to multiple clusters asynchronously (we can do this b/c of the assumption step above).
	go func() {
		bindingCycleCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		metrics.SchedulerGoroutines.WithLabelValues(metrics.Binding).Inc()
		defer metrics.SchedulerGoroutines.WithLabelValues(metrics.Binding).Dec()

		waitOnPermitStatus := sched.framework.WaitOnPermit(bindingCycleCtx, sub)
		if !waitOnPermitStatus.IsSuccess() {
			var reason string
			if waitOnPermitStatus.IsUnschedulable() {
				metrics.SubscriptionUnschedulable(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
				reason = ReasonUnschedulable
			} else {
				metrics.SubscriptionScheduleError(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
				reason = SchedulerError
			}
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			sched.framework.RunReservePluginsUnreserve(bindingCycleCtx, sub, namespacedClusters)
			sched.recordSchedulingFailure(sub, waitOnPermitStatus.AsError(), reason)
			return
		}

		// Run "prebind" plugins.
		preBindStatus := sched.framework.RunPreBindPlugins(bindingCycleCtx, sub, namespacedClusters)
		if !preBindStatus.IsSuccess() {
			metrics.SubscriptionScheduleError(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			sched.framework.RunReservePluginsUnreserve(bindingCycleCtx, sub, namespacedClusters)
			sched.recordSchedulingFailure(sub, preBindStatus.AsError(), SchedulerError)
			return
		}

		err := sched.bind(bindingCycleCtx, sub, namespacedClusters)
		if err != nil {
			metrics.SubscriptionScheduleError(sched.framework.ProfileName(), metrics.SinceInSeconds(start))
			// trigger un-reserve plugins to clean up state associated with the reserved subscription
			sched.framework.RunReservePluginsUnreserve(bindingCycleCtx, sub, namespacedClusters)
			sched.recordSchedulingFailure(sub, fmt.Errorf("binding rejected: %w", err), SchedulerError)
		} else {
			metrics.SubscriptionScheduled(sched.framework.ProfileName(), metrics.SinceInSeconds(start))

			// Run "postbind" plugins.
			sched.framework.RunPostBindPlugins(bindingCycleCtx, sub, namespacedClusters)
		}
	}()
}

// bind a subscription to given clusters.
// We expect this to run asynchronously, so we handle binding metrics internally.
func (sched *Scheduler) bind(ctx context.Context, sub *appsapi.Subscription, targetClusterNamespaces []string) (err error) {
	defer func() {
		// finish binding
		if err != nil {
			klog.V(1).InfoS("Failed to bind sub", "sub", klog.KObj(sub))
			return
		}
		sched.framework.EventRecorder().Eventf(
			sub,
			corev1.EventTypeNormal,
			"Scheduled",
			"Binding",
			"Successfully assigned %s to %s", klog.KObj(sub), strings.Join(targetClusterNamespaces, ","),
		)
	}()

	bindStatus := sched.framework.RunBindPlugins(ctx, sub, targetClusterNamespaces)
	if bindStatus.IsSuccess() {
		return nil
	}
	if bindStatus.Code() == framework.Error {
		return bindStatus.AsError()
	}
	return fmt.Errorf("bind status: %s, %v", bindStatus.Code().String(), bindStatus.Message())
}

// recordSchedulingFailure records an event for the subscription that indicates the
// subscription has failed to schedule. Also, update the subscription condition.
func (sched *Scheduler) recordSchedulingFailure(sub *appsapi.Subscription, err error, _ string) {
	klog.V(2).InfoS("Unable to schedule subscription; waiting", "subscription", klog.KObj(sub), "err", err)

	msg := truncateMessage(err.Error())
	sched.framework.EventRecorder().Event(sub, corev1.EventTypeWarning, "FailedScheduling", msg)

	// TODO: update subscription condition

	// re-added to the queue for re-processing
	sched.SchedulingQueue.AddRateLimited(klog.KObj(sub).String())
}

// addAllEventHandlers is a helper function used in tests and in Scheduler
// to add event handlers for various informers.
func addAllEventHandlers(sched *Scheduler) {
	sched.ClusternetInformerFactory.Apps().V1alpha1().Subscriptions().Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			switch t := obj.(type) {
			case *appsapi.Subscription:
				sub := obj.(*appsapi.Subscription)
				if sub.DeletionTimestamp != nil {
					return false
				}

				// TODO: filter scheduler name
				return true
			case cache.DeletedFinalStateUnknown:
				if _, ok := t.Obj.(*appsapi.Subscription); ok {
					return true
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
				// TODO
				sub := obj.(*appsapi.Subscription)
				sched.SchedulingQueue.AddRateLimited(klog.KObj(sub).String())
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				// TODO
				newSub := newObj.(*appsapi.Subscription)
				sched.SchedulingQueue.AddRateLimited(klog.KObj(newSub).String())
			},
		},
	})
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
