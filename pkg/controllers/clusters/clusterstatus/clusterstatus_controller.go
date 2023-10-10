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
	"strings"
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
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
	utilpointer "k8s.io/utils/pointer"

	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/features"
	"github.com/clusternet/clusternet/pkg/known"
	"github.com/clusternet/clusternet/pkg/utils"
)

// Controller is a controller that collects cluster status
type Controller struct {
	kubeClient         kubernetes.Interface
	metricClientset    *metricsv.Clientset
	lock               *sync.Mutex
	clusterStatus      *clusterapi.ManagedClusterStatus
	collectingPeriod   metav1.Duration
	heartbeatFrequency metav1.Duration
	apiserverURL       string
	appPusherEnabled   bool
	useSocket          bool
	useMetricsServer   bool
	nodeLister         corev1lister.NodeLister
	nodeSynced         cache.InformerSynced
	podLister          corev1lister.PodLister
	podSynced          cache.InformerSynced
	kubeQPS            float32
	kubeBurst          int32

	predictorEnable       bool
	predictorAddress      string
	predictorDirectAccess bool

	labelAggregateThreshold float32
}

func NewController(
	apiserverURL string,
	kubeClient kubernetes.Interface,
	metricClient *metricsv.Clientset,
	kubeInformerFactory informers.SharedInformerFactory,
	predictorAddress string,
	predictorDirectAccess, useMetricsServer bool,
	collectingPeriod metav1.Duration,
	heartbeatFrequency metav1.Duration,
	labelAggregateThreshold float32,
	kubeQPS float32,
	kubeBurst int32,
) *Controller {
	return &Controller{
		kubeClient:              kubeClient,
		metricClientset:         metricClient,
		lock:                    &sync.Mutex{},
		collectingPeriod:        collectingPeriod,
		heartbeatFrequency:      heartbeatFrequency,
		apiserverURL:            apiserverURL,
		labelAggregateThreshold: labelAggregateThreshold,
		appPusherEnabled:        utilfeature.DefaultFeatureGate.Enabled(features.AppPusher),
		useSocket:               utilfeature.DefaultFeatureGate.Enabled(features.SocketConnection),
		predictorEnable:         utilfeature.DefaultFeatureGate.Enabled(features.Predictor),
		useMetricsServer:        useMetricsServer,
		predictorAddress:        predictorAddress,
		predictorDirectAccess:   predictorDirectAccess,
		kubeQPS:                 kubeQPS,
		kubeBurst:               kubeBurst,
		nodeLister:              kubeInformerFactory.Core().V1().Nodes().Lister(),
		nodeSynced:              kubeInformerFactory.Core().V1().Nodes().Informer().HasSynced,
		podLister:               kubeInformerFactory.Core().V1().Pods().Lister(),
		podSynced:               kubeInformerFactory.Core().V1().Pods().Informer().HasSynced,
	}
}

func (c *Controller) Run(ctx context.Context) {
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
	var status clusterapi.ManagedClusterStatus

	clusterVersion, err := c.getKubernetesVersion(ctx)
	if err != nil {
		klog.Warningf("failed to collect kubernetes version: %v", err)
	} else {
		status.KubernetesVersion = clusterVersion.GitVersion
		status.Platform = clusterVersion.Platform
	}

	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list nodes: %v", err)
	} else {
		status.NodeStatistics = getNodeStatistics(nodes)

		capacity, allocatable := getNodeResource(nodes)
		status.Allocatable = allocatable
		status.Capacity = capacity
	}

	clusterCIDR, err := c.discoverClusterCIDR()
	if err != nil {
		klog.Warningf("failed to discover cluster CIDR: %v", err)
	} else {
		status.ClusterCIDR = clusterCIDR
	}

	serviceCIDR, err := c.discoverServiceCIDR()
	if err != nil {
		klog.Warningf("failed to discover service CIDR: %v", err)
	} else {
		status.ServiceCIDR = serviceCIDR
	}

	status.APIServerURL = c.apiserverURL
	status.Healthz = c.getHealthStatus(ctx, "/healthz")
	status.Livez = c.getHealthStatus(ctx, "/livez")
	status.Readyz = c.getHealthStatus(ctx, "/readyz")

	status.AppPusher = &c.appPusherEnabled
	status.UseSocket = c.useSocket

	if c.useMetricsServer {
		status.PodStatistics = getPodStatistics(c.metricClientset)
		status.ResourceUsage = getResourceUsage(c.metricClientset)
	}

	status.HeartbeatFrequencySeconds = utilpointer.Int64(int64(c.heartbeatFrequency.Seconds()))
	status.Conditions = []metav1.Condition{c.getCondition(status)}

	status.KubeQPS = c.kubeQPS
	status.KubeBurst = c.kubeBurst

	status.PredictorEnabled = c.predictorEnable
	status.PredictorAddress = c.predictorAddress
	status.PredictorDirectAccess = c.predictorDirectAccess

	c.setClusterStatus(status)
}

func (c *Controller) setClusterStatus(status clusterapi.ManagedClusterStatus) {
	c.lock.Lock()
	defer c.lock.Unlock()

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

func (c *Controller) GetManagedClusterLabels(labelPrefixes []string) labels.Set {
	nodes, err := c.nodeLister.List(labels.Everything())
	if err != nil {
		klog.Warningf("failed to list nodes: %v", err)
		return nil
	}
	return aggregateLimitedLabels(nodes, c.labelAggregateThreshold, labelPrefixes)
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

// getPodStatistics returns the PodStatistics in the cluster
// get pods num in running conditions and the total pods num in the cluster
func getPodStatistics(clientset *metricsv.Clientset) *clusterapi.PodStatistics {
	if clientset == nil {
		klog.Warningf("empty metrics client, will return directly")
		return nil
	}

	podMetricsList, err := clientset.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Warningf("failed to list podMetrics with err: %v", err)
		return nil
	}
	podStatistics := &clusterapi.PodStatistics{}
	for _, item := range podMetricsList.Items {
		if len(item.Containers) != 0 {
			podStatistics.RunningPods += 1
		}
	}
	podStatistics.TotalPods = int32(len(podMetricsList.Items))

	return podStatistics
}

// getResourceUsage returns the ResourceUsage in the cluster
// get cpu(m) and memory(Mi) used
func getResourceUsage(clientset *metricsv.Clientset) *clusterapi.ResourceUsage {
	if clientset == nil {
		klog.Warningf("empty metrics client, will return directly")
		return nil
	}

	nodeMetricsList, err := clientset.MetricsV1beta1().NodeMetricses().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Warningf("failed to list nodeMetrics with err: %v", err)
		return nil
	}
	resourceUsage := &clusterapi.ResourceUsage{}
	for _, item := range nodeMetricsList.Items {
		resourceUsage.CpuUsage.Add(*(item.Usage.Cpu()))
		resourceUsage.MemoryUsage.Add(*(item.Usage.Memory()))
	}

	return resourceUsage
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

// getCommonNodeLabels return the common labels from nodes meta.label
func getCommonNodeLabels(nodes []*corev1.Node) map[string]string {
	if len(nodes) == 0 {
		return nil
	}
	initLabels := nodes[0].Labels
	for _, node := range nodes {
		if isMasterNode(node.Labels) {
			continue
		}
		currentLabels := map[string]string{}
		for k, v := range node.Labels {
			c, ok := initLabels[k]
			if ok && c == v {
				if strings.HasPrefix(k, known.NodeLabelsKeyPrefix) {
					currentLabels[k] = v
				}
			}
		}
		if len(currentLabels) == 0 {
			return nil
		}
		initLabels = currentLabels
	}
	return initLabels
}

// isMasterNode return true if the node is a master node.
func isMasterNode(labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}
	if _, ok := labels["node-role.kubernetes.io/control-plane"]; ok {
		return true
	}
	if _, ok := labels["node-role.kubernetes.io/master"]; ok {
		return true
	}
	return false
}

func statisticNodesLabels(nodes []*corev1.Node, labelPrefixes []string) (map[string]int, int) {
	// statistic map.
	countMap := make(map[string]map[string]int, 0)
	masterNodeNum := 0
	for _, node := range nodes {
		if isMasterNode(node.Labels) {
			masterNodeNum += 1
			continue
		}
		for labelKey, labelValue := range node.Labels {
			if utils.ContainsPrefix(labelPrefixes, labelKey) {
				label := strings.Join([]string{labelKey, labelValue}, "=")
				if labelMap, isok := countMap[labelKey]; isok {
					if count, isok2 := labelMap[label]; isok2 {
						countMap[labelKey][label] = count + 1
					} else {
						countMap[labelKey][label] = 1
					}
				} else {
					countMap[labelKey] = map[string]int{label: 1}
				}
			}
		}
	}
	return filterMaxPair(countMap), masterNodeNum
}

func filterMaxPair(m map[string]map[string]int) map[string]int {
	result := make(map[string]int)
	for _, labelMap := range m {
		var label string
		var count int
		for k, v := range labelMap {
			if count < v {
				label = k
				count = v
			}
		}
		result[label] = count
	}
	return result
}

func aggregateLimitedLabels(nodes []*corev1.Node, threshold float32, labelPrefixes []string) map[string]string {
	newMap := make(map[string]string, 0)
	countMap, masterNum := statisticNodesLabels(nodes, labelPrefixes)
	workNodeNum := len(nodes) - masterNum
	for k, v := range countMap {
		// the key is higher than threshold
		if float32(v)/float32(workNodeNum) >= threshold {
			lastInd := strings.LastIndex(k, "=")
			newMap[k[:lastInd]] = k[lastInd+1:]
		}
	}
	return newMap
}
