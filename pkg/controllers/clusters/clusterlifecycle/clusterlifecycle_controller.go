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

package clusterlifecycle

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dixudx/yacht"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	utilptr "k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	clusternetclientset "github.com/clusternet/clusternet/pkg/generated/clientset/versioned"
	appsinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/apps/v1alpha1"
	clusterinformers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions/clusters/v1beta1"
	appslister "github.com/clusternet/clusternet/pkg/generated/listers/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
	taintutils "github.com/clusternet/clusternet/pkg/utils/taints"
)

const (
	DefaultClusterMonitorGracePeriod = 9 * time.Minute

	defaultClusterInitGracePeriod = 30 * time.Second

	// ClusterNotReadyThreshold indicate the threshold for cluster not ready period
	ClusterNotReadyThreshold = 3
)

var (
	// map {ConditionType: {ConditionStatus: TaintKey}}
	// represents which ConditionType under which ConditionStatus should be
	// tainted with which TaintKey
	// for certain ConditionType, there are multiple {ConditionStatus,TaintKey} pairs
	clusterConditionToTaintKeyStatusMap = map[string]map[metav1.ConditionStatus]string{
		clusterapi.ClusterReady: {
			metav1.ConditionFalse:   known.TaintClusterUnschedulable,
			metav1.ConditionUnknown: known.TaintClusterUnschedulable,
		},
		clusterapi.ClusterInit: {
			metav1.ConditionFalse:   known.TaintClusterInitialization,
			metav1.ConditionUnknown: known.TaintClusterInitialization,
		},
	}

	taintKeyToNodeConditionMap = map[string]string{
		known.TaintClusterUnschedulable:  clusterapi.ClusterReady,
		known.TaintClusterInitialization: clusterapi.ClusterInit,
	}
)

var (
	// subtleClusterInitAnnotations includes some known annotations that we care for cluster initialization
	subtleClusterInitAnnotations = []string{
		known.ClusterInitSkipAnnotation,
		known.ClusterInitBaseAnnotation,
	}
)

var (
	baseKind = appsapi.SchemeGroupVersion.WithKind("Base")
)

// Controller is a controller that manages cluster's lifecycle
type Controller struct {
	clusternetClient clusternetclientset.Interface
	recorder         record.EventRecorder
	yachtController  *yacht.Controller
	clusterInformer  clusterinformers.ManagedClusterInformer
	baseInformer     appsinformers.BaseInformer
	descLister       appslister.DescriptionLister

	clusterInitBaseMap map[string]baseInfo
	lock               *sync.Mutex
}

type baseInfo struct {
	baseName    string
	createdTime metav1.Time
}

func NewController(
	clusternetClient clusternetclientset.Interface,
	clusterInformer clusterinformers.ManagedClusterInformer,
	baseInformer appsinformers.BaseInformer,
	descLister appslister.DescriptionLister,
	recorder record.EventRecorder,
) (*Controller, error) {
	c := &Controller{
		clusternetClient:   clusternetClient,
		recorder:           recorder,
		clusterInformer:    clusterInformer,
		baseInformer:       baseInformer,
		descLister:         descLister,
		clusterInitBaseMap: make(map[string]baseInfo),
		lock:               &sync.Mutex{},
	}

	yachtController := yacht.NewController("cluster-lifecycle").
		WithHandlerFunc(c.handle).
		WithEnqueueFilterFunc(func(oldObj, newObj interface{}) (bool, error) {
			// UPDATE: status change
			if oldObj != nil && newObj != nil {
				oldMcls := oldObj.(*clusterapi.ManagedCluster)
				newMcls := newObj.(*clusterapi.ManagedCluster)
				if newMcls.DeletionTimestamp != nil {
					return true, nil
				}

				if oldMcls.Spec.ClusterInitBaseName != newMcls.Spec.ClusterInitBaseName {
					return true, nil
				}

				// For cluster init, we care some known annotation changes
				for _, annotation := range subtleClusterInitAnnotations {
					if oldMcls.Annotations[annotation] != newMcls.Annotations[annotation] {
						return true, nil
					}
				}

				// Decide whether discovery has reported a status change.
				if equality.Semantic.DeepEqual(oldMcls.Status, newMcls.Status) {
					klog.V(4).Infof("no updates on the status of ManagedCluster %s, skipping syncing", klog.KObj(oldMcls))
					return false, nil
				}
			}

			// ADD/DELETE/OTHER UPDATE
			return true, nil
		})

	// add event handler for ManagedCluster on the addition/update
	clusterEventHandler := yachtController.DefaultResourceEventHandlerFuncs()
	clusterEventHandler.DeleteFunc = nil // only care cluster add/update
	_, err := clusterInformer.Informer().AddEventHandler(clusterEventHandler)
	if err != nil {
		return nil, err
	}

	_, err = baseInformer.Informer().AddEventHandler(cache.FilteringResourceEventHandler{
		FilterFunc: func(obj interface{}) bool {
			// we always return false in this filter func
			c.cacheClusterInitBaseInfo(obj)
			return false
		},
		// we don't need any special handlers here, since we always return false in FilterFunc
		Handler: cache.ResourceEventHandlerFuncs{},
	})
	if err != nil {
		return nil, err
	}

	c.yachtController = yachtController
	return c, nil
}

func (c *Controller) Run(workers int, ctx context.Context) {
	defer utilruntime.HandleCrash()

	klog.Infof("Starting cluster lifecycle controller")
	defer klog.Infof("Shutting down cluster lifecycle controller")

	c.yachtController.WithWorkers(workers).Run(ctx)
}

func (c *Controller) handle(key interface{}) (requeueAfter *time.Duration, err error) {
	// Convert the namespace/name string into a distinct namespace and name
	ns, name, err := cache.SplitMetaNamespaceKey(key.(string))
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil, nil
	}

	klog.V(4).Infof("start processing ManagedCluster %q", key)
	// Get the ManagedCluster resource with this name
	mcls, err := c.clusterInformer.Lister().ManagedClusters(ns).Get(name)
	// The ManagedCluster resource may no longer exist, in which case we stop processing.
	if apierrors.IsNotFound(err) {
		klog.V(2).Infof("ManagedCluster %q has been deleted", key)
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if mcls.DeletionTimestamp != nil {
		klog.V(2).Infof("ManagedCluster %q is being deleted. Skip it", key)
		return nil, nil
	}

	updatedMcls, err := c.updateClusterConditions(context.TODO(), mcls.DeepCopy())
	if err != nil {
		msg := fmt.Sprintf("failed to update conditions of ManagedCluster status: %v", err)
		klog.WarningDepth(2, msg)
		c.recorder.Event(mcls, corev1.EventTypeWarning, "FailedUpdatingClusterStatus", msg)
		return nil, err
	}

	err = c.updateClusterTaintsAndInitBase(context.TODO(), updatedMcls)
	if err != nil {
		return nil, err
	}

	// requeueAfter is the duration that we consider a cluster to be requeued, such as cluster lost, cluster init, etc.
	return getGracefulRequeuePeriod(updatedMcls.Status), nil
}

func (c *Controller) updateClusterConditions(ctx context.Context, cluster *clusterapi.ManagedCluster) (*clusterapi.ManagedCluster, error) {
	var newConditions []metav1.Condition
	observedReadyCondition := generateClusterReadyCondition(cluster)
	if observedReadyCondition != nil {
		newConditions = append(newConditions, *observedReadyCondition)

		// update Readyz/Livez/Healthz to false when cluster status is not ready
		if observedReadyCondition.Status != metav1.ConditionTrue {
			cluster.Status.Readyz = false
			cluster.Status.Livez = false
			cluster.Status.Healthz = false
		}
	}

	clusterInitCondition := generateClusterInitCondition(cluster, c.baseInformer.Lister(), c.descLister)
	if clusterInitCondition != nil {
		newConditions = append(newConditions, *clusterInitCondition)
	}

	hasChanged := utils.UpdateConditions(&cluster.Status, newConditions)
	if !hasChanged {
		return cluster, nil
	}

	return c.clusternetClient.ClustersV1beta1().ManagedClusters(cluster.Namespace).UpdateStatus(ctx,
		cluster, metav1.UpdateOptions{})
}

func (c *Controller) updateClusterTaintsAndInitBase(ctx context.Context, cluster *clusterapi.ManagedCluster) error {
	// Map cluster's condition to Taints.
	var taints []corev1.Taint
	for _, condition := range cluster.Status.Conditions {
		if !utilfeature.DefaultFeatureGate.Enabled(features.ClusterInit) && condition.Type == clusterapi.ClusterInit {
			continue
		}

		if taintMap, found := clusterConditionToTaintKeyStatusMap[condition.Type]; found {
			if taintKey, found2 := taintMap[condition.Status]; found2 {
				taints = append(taints, corev1.Taint{
					Key:    taintKey,
					Value:  condition.Reason,
					Effect: corev1.TaintEffectNoSchedule,
				})
			}
		}
	}

	// Get exist taints of cluster.
	clusterTaints := taintutils.TaintSetFilter(cluster.Spec.Taints, func(t *corev1.Taint) bool {
		// only NoSchedule taints are candidates to be compared with "taints" later
		if t.Effect != corev1.TaintEffectNoSchedule {
			return false
		}
		// Find unschedulable taint of cluster.
		if t.Key == known.TaintClusterUnschedulable {
			return true
		}
		// Find cluster condition taints of cluster.
		_, found := taintKeyToNodeConditionMap[t.Key]
		return found
	})
	taintsToAdd, taintsToDel := taintutils.TaintSetDiff(taints, clusterTaints)

	// patch taints & initBase
	return patchClusterTaintsAndInitBase(ctx, c.clusternetClient, cluster, taintsToAdd, taintsToDel,
		c.getClusterInitBase(cluster))
}

func (c *Controller) cacheClusterInitBaseInfo(obj interface{}) {
	base, ok := obj.(*appsapi.Base)
	if !ok || base.DeletionTimestamp != nil {
		return
	}

	isDefault, ok2 := base.Annotations[known.IsDefaultClusterInitAnnotation]
	if !ok2 || isDefault != "true" {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	cur, ok3 := c.clusterInitBaseMap[base.Namespace]
	if !ok3 || base.CreationTimestamp.Time.Before(cur.createdTime.Time) {
		return
	}

	// select the latest from default Base candidates
	c.clusterInitBaseMap[base.Namespace] = baseInfo{baseName: base.Name, createdTime: base.CreationTimestamp}
	return
}

func (c *Controller) getClusterInitBase(cluster *clusterapi.ManagedCluster) *string {
	if isTrue, ok := cluster.Annotations[known.ClusterInitSkipAnnotation]; ok && isTrue == "true" {
		return nil
	}

	// For legacy clusters, it is strongly suggested to perform ClusterInit through annotation.
	if base, ok := cluster.Annotations[known.ClusterInitBaseAnnotation]; ok {
		return utilptr.String(base)
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	if info, ok := c.clusterInitBaseMap[cluster.Namespace]; ok {
		return utilptr.String(info.baseName)
	}

	return nil
}

func patchClusterTaintsAndInitBase(
	ctx context.Context,
	client clusternetclientset.Interface,
	cluster *clusterapi.ManagedCluster,
	taintsToAdd, taintsToRemove []*corev1.Taint,
	initBase *string,
) error {
	var err error
	newCluster := cluster.DeepCopy()
	for _, taint := range taintsToRemove {
		newCluster, _, err = taintutils.RemoveTaint(cluster, taint)
		if err != nil {
			return err
		}
	}
	for _, taint := range taintsToAdd {
		newCluster, _, err = taintutils.AddOrUpdateTaint(newCluster, taint)
		if err != nil {
			return err
		}
	}

	// we won't mutate ClusterInitBaseName
	if initBase != nil && newCluster.Spec.ClusterInitBaseName == nil {
		newCluster.Spec.ClusterInitBaseName = initBase
	}

	return patchCluster(ctx, client, cluster, newCluster)
}

func patchCluster(
	ctx context.Context,
	client clusternetclientset.Interface,
	oldCluster, newCluster *clusterapi.ManagedCluster,
) error {
	// patch ManagedCluster
	oldData, err := utils.Marshal(oldCluster)
	if err != nil {
		return err
	}
	newData, err := utils.Marshal(newCluster)
	if err != nil {
		return err
	}
	patchBytes, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, clusterapi.ManagedCluster{})
	if err != nil {
		return err
	}
	if len(patchBytes) == 0 {
		return nil
	}
	_, err = client.ClustersV1beta1().ManagedClusters(oldCluster.Namespace).Patch(
		ctx,
		oldCluster.Name,
		types.MergePatchType,
		patchBytes,
		metav1.PatchOptions{},
	)
	return err
}

// generateClusterReadyCondition return cluster unknown condition based on cluster status and grace period.
func generateClusterReadyCondition(cluster *clusterapi.ManagedCluster) *metav1.Condition {
	lastObservedTime := cluster.Status.LastObservedTime
	probeTimestamp := lastObservedTime
	if lastObservedTime.IsZero() {
		// If lastObservedTime is zero means Clusternet agent never posted cluster status.
		// We treat this cluster to be a new cluster and use cluster.CreationTimestamp to be probeTimestamp.
		probeTimestamp = cluster.CreationTimestamp
	}

	if !metav1.Now().After(probeTimestamp.Add(*getHeartbeatThresholdPeriod(cluster.Status))) {
		return nil
	}

	if lastObservedTime.IsZero() {
		// Clusternet agent never posted cluster status and reach grace period
		return &metav1.Condition{
			Type:               clusterapi.ClusterReady,
			Status:             metav1.ConditionUnknown,
			LastTransitionTime: metav1.Now(),
			Reason:             "ClusterStatusNeverUpdated",
			Message:            "Clusternet agent never posted cluster status.",
		}
	}
	// Clusternet agent is stopping posting cluster status
	return &metav1.Condition{
		Type:               clusterapi.ClusterReady,
		Status:             metav1.ConditionUnknown,
		LastTransitionTime: metav1.Now(),
		Reason:             "ClusterStatusUnknown",
		Message:            "Clusternet agent stopped posting cluster status.",
	}
}

func generateClusterInitCondition(
	cluster *clusterapi.ManagedCluster,
	baseLister appslister.BaseLister,
	descLister appslister.DescriptionLister,
) *metav1.Condition {
	condition := &metav1.Condition{
		Type:               clusterapi.ClusterInit,
		LastTransitionTime: metav1.Now(),
	}

	if !utilfeature.DefaultFeatureGate.Enabled(features.ClusterInit) {
		// always return true condition
		// Don't bother to prune this condition when this feature gate is disabled
		condition.Status = metav1.ConditionTrue
		condition.Reason = known.ClusterInitDisabledReason
		condition.Message = "feature gate ClusterInit is not enabled"
		return condition
	}

	// skip initializing the cluster when annotation ClusterInitSkipAnnotation is seen
	if isTrue, ok := cluster.Annotations[known.ClusterInitSkipAnnotation]; ok && isTrue == "true" {
		condition.Status = metav1.ConditionTrue
		condition.Reason = known.ClusterInitDisabledReason
		condition.Message = "cluster initialization is skipped"
		return condition
	}

	clusterInitResult, msg := getClusterInitResult(cluster.Namespace, cluster.Spec.ClusterInitBaseName, baseLister, descLister)
	if !clusterInitResult {
		condition.Status = metav1.ConditionFalse
		condition.Reason = known.ClusterInitWaitingReason
		condition.Message = msg
		return condition
	}

	// on success
	condition.Status = metav1.ConditionTrue
	condition.Reason = known.ClusterInitDoneReason
	condition.Message = "cluster has been initialized successfully"
	return condition
}

func getClusterInitResult(
	baseNamespace string,
	baseName *string,
	baseLister appslister.BaseLister,
	descLister appslister.DescriptionLister,
) (bool, string) {
	if baseName == nil {
		return false, "still waiting for cluster getting initialized"
	}

	if len(*baseName) == 0 {
		klog.Errorf("[cluster-init] found empty base in namespace %s", baseNamespace)
		return false, "found empty base"
	}

	base, err := baseLister.Bases(baseNamespace).Get(*baseName)
	if err != nil {
		klog.Errorf("[cluster-init] failed to get Base %s/%s", baseNamespace, *baseName)
		return false, "failed to get Base, will retry"
	}

	descs, err := descLister.Descriptions(baseNamespace).List(labels.SelectorFromSet(labels.Set{
		known.ConfigKindLabel:      baseKind.Kind,
		known.ConfigNamespaceLabel: base.Namespace,
		known.ConfigUIDLabel:       string(base.UID),
	}))
	if err != nil {
		klog.Errorf("[cluster-init] failed to list Description matching Base %s/%s", baseNamespace, *baseName)
		return false, "failed to list Description, will retry"
	}
	if len(descs) == 0 {
		return false, "no matching Descriptions are found. will retry"
	}

	successStatus := true
	var msg []string
	for _, desc := range descs {
		successStatus = successStatus && (desc.Status.Phase == appsapi.DescriptionPhaseSuccess)
		if !successStatus {
			msg = append(msg, desc.Status.Reason)
		}
	}

	return successStatus, strings.Join(msg, ", ")
}

// getGracefulRequeuePeriod calculate grace period for cluster initialization and monitoring.
func getGracefulRequeuePeriod(status clusterapi.ManagedClusterStatus) *time.Duration {
	if utilfeature.DefaultFeatureGate.Enabled(features.ClusterInit) {
		clusterInitGracePeriod := defaultClusterInitGracePeriod
		clusterInitCondition := utils.GetCondition(status.Conditions, clusterapi.ClusterInit)
		if clusterInitCondition == nil || clusterInitCondition.Status != metav1.ConditionTrue {
			return &clusterInitGracePeriod
		}
	}
	return getHeartbeatThresholdPeriod(status)
}

func getHeartbeatThresholdPeriod(status clusterapi.ManagedClusterStatus) *time.Duration {
	// use DefaultClusterMonitorGracePeriod if HeartbeatFrequencySeconds is undefined.
	gracePeriod := DefaultClusterMonitorGracePeriod
	if status.HeartbeatFrequencySeconds != nil {
		gracePeriod = time.Second * time.Duration(*status.HeartbeatFrequencySeconds) * ClusterNotReadyThreshold
	}
	return &gracePeriod
}
