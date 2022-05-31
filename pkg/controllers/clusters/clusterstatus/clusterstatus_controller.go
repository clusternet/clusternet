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

package clusterstatus

import (
	"context"
	"net/http"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/version"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1lister "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	utilpointer "k8s.io/utils/pointer"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/known"
)

// Controller is a controller that collects cluster status
type Controller struct {
	kubeClient         kubernetes.Interface
	lock               *sync.Mutex
	clusterStatus      *clusterapi.ManagedClusterStatus
	collectingPeriod   metav1.Duration
	heartbeatFrequency metav1.Duration
	apiserverURL       string
	appPusherEnabled   bool
	useSocket          bool
	nodeLister         corev1lister.NodeLister
	nodeSynced         cache.InformerSynced
	podLister          corev1lister.PodLister
	podSynced          cache.InformerSynced

	kubeInformerFactory informers.SharedInformerFactory
}

func NewController(apiserverURL string, kubeClient kubernetes.Interface, collectingPeriod metav1.Duration, heartbeatFrequency metav1.Duration) *Controller {
	kubeInformerFactory := informers.NewSharedInformerFactory(kubeClient, known.DefaultResync)
	return &Controller{
		kubeClient:          kubeClient,
		lock:                &sync.Mutex{},
		collectingPeriod:    collectingPeriod,
		heartbeatFrequency:  heartbeatFrequency,
		apiserverURL:        apiserverURL,
		appPusherEnabled:    utilfeature.DefaultFeatureGate.Enabled(features.AppPusher),
		useSocket:           utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection),
		nodeLister:          kubeInformerFactory.Core().V1().Nodes().Lister(),
		nodeSynced:          kubeInformerFactory.Core().V1().Nodes().Informer().HasSynced,
		podLister:           kubeInformerFactory.Core().V1().Pods().Lister(),
		podSynced:           kubeInformerFactory.Core().V1().Pods().Informer().HasSynced,
		kubeInformerFactory: kubeInformerFactory,
	}
}

func (c *Controller) Run(ctx context.Context) {
	c.kubeInformerFactory.Start(ctx.Done())

	if !cache.WaitForNamedCacheSync("cluster-status-controller", ctx.Done(),
		c.podSynced,
		c.nodeSynced,
	) {
		return
	}

	wait.UntilWithContext(ctx, c.collectingClusterStatus, c.collectingPeriod.Duration)
}

func (c *Controller) collectingClusterStatus(ctx context.Context) {
	klog.V(7).Info("collecting cluster status...")
	clusterVersion, err := c.getKubernetesVersion(ctx)
	if err != nil {
		klog.Warningf("failed to collect kubernetes version: %v", err)
	}

	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list nodes: %v", err)
	}

	nodeStatistics := getNodeStatistics(nodes)

	capacity, allocatable := getNodeResource(nodes)

	clusterCIDR, err := c.discoverClusterCIDR()
	if err != nil {
		klog.Warningf("failed to discover cluster CIDR: %v", err)
	}

	serviceCIDR, err := c.discoverServiceCIDR()
	if err != nil {
		klog.Warningf("failed to discover service CIDR: %v", err)
	}

	var status clusterapi.ManagedClusterStatus
	status.KubernetesVersion = clusterVersion.GitVersion
	status.Platform = clusterVersion.Platform
	status.APIServerURL = c.apiserverURL
	status.Healthz = c.getHealthStatus(ctx, "/healthz")
	status.Livez = c.getHealthStatus(ctx, "/livez")
	status.Readyz = c.getHealthStatus(ctx, "/readyz")
	status.AppPusher = c.appPusherEnabled
	status.UseSocket = c.useSocket
	status.ClusterCIDR = clusterCIDR
	status.ServiceCIDR = serviceCIDR
	status.NodeStatistics = nodeStatistics
	status.Allocatable = allocatable
	status.Capacity = capacity
	status.HeartbeatFrequencySeconds = utilpointer.Int64Ptr(int64(c.heartbeatFrequency.Seconds()))
	status.Conditions = []metav1.Condition{c.getCondition(status)}
	c.setClusterStatus(status)
}

func (c *Controller) setClusterStatus(status clusterapi.ManagedClusterStatus) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.clusterStatus == nil {
		c.clusterStatus = new(clusterapi.ManagedClusterStatus)
	}

	c.clusterStatus = &status
	c.clusterStatus.LastObservedTime = metav1.Now()
	klog.V(7).Infof("current cluster status is %#v", status)
}

func (c *Controller) GetClusterStatus() *clusterapi.ManagedClusterStatus {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.clusterStatus == nil {
		return nil
	}

	return c.clusterStatus.DeepCopy()
}

func (c *Controller) getKubernetesVersion(_ context.Context) (*version.Info, error) {
	return c.kubeClient.Discovery().ServerVersion()
}

func (c *Controller) getHealthStatus(ctx context.Context, path string) bool {
	var statusCode int
	c.kubeClient.Discovery().RESTClient().Get().AbsPath(path).Do(ctx).StatusCode(&statusCode)
	return statusCode == http.StatusOK
}

func (c *Controller) getCondition(status clusterapi.ManagedClusterStatus) metav1.Condition {
	if (status.Livez && status.Readyz) || status.Healthz {
		return metav1.Condition{
			Type:               clusterapi.ClusterReady,
			Status:             metav1.ConditionTrue,
			LastTransitionTime: metav1.Now(),
			Reason:             "ManagedClusterReady",
			Message:            "managed cluster is ready.",
		}
	}

	return metav1.Condition{
		Type:               clusterapi.ClusterReady,
		Status:             metav1.ConditionFalse,
		LastTransitionTime: metav1.Now(),
		Reason:             "ManagedClusterNotReady",
		Message:            "managed cluster is not ready.",
	}
}

// getNodeStatistics returns the NodeStatistics in the cluster
// get nodes num in different conditions
func getNodeStatistics(nodes []*corev1.Node) (nodeStatistics clusterapi.NodeStatistics) {
	for _, node := range nodes {
		flag, condition := getNodeCondition(&node.Status, corev1.NodeReady)
		if flag == -1 {
			nodeStatistics.LostNodes += 1
			continue
		}

		switch condition.Status {
		case corev1.ConditionTrue:
			nodeStatistics.ReadyNodes += 1
		case corev1.ConditionFalse:
			nodeStatistics.NotReadyNodes += 1
		case corev1.ConditionUnknown:
			nodeStatistics.UnknownNodes += 1
		}
	}
	return
}

// discoverServiceCIDR returns the service CIDR for the cluster.
func (c *Controller) discoverServiceCIDR() (string, error) {
	return findServiceIPRange(c.podLister)
}

// discoverClusterCIDR returns the cluster CIDR for the cluster.
func (c *Controller) discoverClusterCIDR() (string, error) {
	return findPodIPRange(c.nodeLister, c.podLister)
}

// get node capacity and allocatable resource
func getNodeResource(nodes []*corev1.Node) (Capacity, Allocatable corev1.ResourceList) {
	var capacityCpu, capacityMem, capacityGpu, allocatableCpu, allocatableMem, allocatableGpu resource.Quantity
	Capacity, Allocatable = make(map[corev1.ResourceName]resource.Quantity), make(map[corev1.ResourceName]resource.Quantity)

	for _, node := range nodes {
		capacityCpu.Add(*node.Status.Capacity.Cpu())
		capacityMem.Add(*node.Status.Capacity.Memory())
		allocatableCpu.Add(*node.Status.Allocatable.Cpu())
		allocatableMem.Add(*node.Status.Allocatable.Memory())
		if _, exists := node.Status.Capacity[known.NVIDIAGPUResourceName]; exists {
			capacityGpu.Add(node.Status.Capacity[corev1.ResourceName(known.NVIDIAGPUResourceName)])
			allocatableGpu.Add(node.Status.Allocatable[corev1.ResourceName(known.NVIDIAGPUResourceName)])
		}
	}

	Capacity[corev1.ResourceCPU] = capacityCpu
	Capacity[corev1.ResourceMemory] = capacityMem
	Allocatable[corev1.ResourceCPU] = allocatableCpu
	Allocatable[corev1.ResourceMemory] = allocatableMem
	if !capacityGpu.IsZero() {
		Capacity[corev1.ResourceName(known.NVIDIAGPUResourceName)] = capacityGpu
		Allocatable[corev1.ResourceName(known.NVIDIAGPUResourceName)] = allocatableGpu
	}

	return
}

// getNodeCondition returns the specified condition from node's status
// Copied from k8s.io/kubernetes/pkg/controller/util/node/controller_utils.go and make some modifications
func getNodeCondition(status *corev1.NodeStatus, conditionType corev1.NodeConditionType) (int, *corev1.NodeCondition) {
	if status == nil {
		return -1, nil
	}
	for i := range status.Conditions {
		if status.Conditions[i].Type == conditionType {
			return i, &status.Conditions[i]
		}
	}
	return -1, nil
}
