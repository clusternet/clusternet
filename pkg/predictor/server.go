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

package predictor

import (
	"context"
	"fmt"
	"sync/atomic"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	informerv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	listerv1 "k8s.io/client-go/listers/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	"github.com/clusternet/clusternet/pkg/known"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/predictor/framework/plugins"
	frameworkruntime "github.com/clusternet/clusternet/pkg/predictor/framework/runtime"
	"github.com/clusternet/clusternet/pkg/scheduler/parallelize"
)

const (
	nodeNameKeyIndex = "spec.nodeName"
)

// GetPodsAssignedToNodeFunc is a function which accept a node name input
// and returns the pods that assigned to the node.
type GetPodsAssignedToNodeFunc func(string) ([]*corev1.Pod, error)

type PredictorServer struct {
	ctx context.Context

	kubeClient kubernetes.Interface

	informerFactory informers.SharedInformerFactory
	nodeInformer    informerv1.NodeInformer
	podInformer     informerv1.PodInformer
	nodeLister      listerv1.NodeLister
	podLister       listerv1.PodLister

	getPodFunc GetPodsAssignedToNodeFunc

	// default in-tree registry
	registry  frameworkruntime.Registry
	framework framework.Framework
}

func NewPredictorServer(
	ctx context.Context,
	clientConfig *restclient.Config,
) (*PredictorServer, error) {
	// creating the clientset
	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: clientConfig,
	}
	kubeClient := kubernetes.NewForConfigOrDie(rootClientBuilder.ConfigOrDie("clusternet-predictor"))
	informerFactory := informers.NewSharedInformerFactory(kubeClient, known.DefaultResync)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{
		Interface: kubeClient.CoreV1().Events(""),
	})
	utilruntime.Must(appsapi.AddToScheme(scheme.Scheme))
	utilruntime.Must(clusterapi.AddToScheme(scheme.Scheme))
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusternet-predictor"})
	ps := &PredictorServer{
		ctx:             ctx,
		kubeClient:      kubeClient,
		informerFactory: informerFactory,
		nodeInformer:    informerFactory.Core().V1().Nodes(),
		podInformer:     informerFactory.Core().V1().Pods(),
		nodeLister:      informerFactory.Core().V1().Nodes().Lister(),
		podLister:       informerFactory.Core().V1().Pods().Lister(),
		registry:        plugins.NewInTreeRegistry(),
	}

	framework, err := frameworkruntime.NewFramework(ps.registry, getDefaultPlugins(),
		frameworkruntime.WithEventRecorder(recorder),
		frameworkruntime.WithInformerFactory(informerFactory),
		frameworkruntime.WithClientSet(kubeClient),
		frameworkruntime.WithKubeConfig(clientConfig),
		frameworkruntime.WithParallelism(parallelize.DefaultParallelism),
		frameworkruntime.WithRunAllFilters(false),
	)
	if err != nil {
		return nil, err
	}
	ps.framework = framework

	getPodFunc, err := ps.BuildGetPodsAssignedToNodeFunc(ps.podInformer)
	if err != nil {
		return nil, err
	}
	ps.getPodFunc = getPodFunc

	return ps, nil
}

var _ schedulerapi.PredictorProvider = &PredictorServer{}

func (ps *PredictorServer) Run() error {
	// Start all informers.
	ps.informerFactory.Start(ps.ctx.Done())
	// Wait for all caches to sync before scheduling.
	ps.informerFactory.WaitForCacheSync(ps.ctx.Done())

	ps.run(ps.ctx)

	return nil
}

func (ps *PredictorServer) run(ctx context.Context) {
	stopCh := ctx.Done()
	klog.Infof("Starting clusternet predictor")
	defer klog.Infof("Shutting down clusternet predictor")

	ps.informerFactory.Start(stopCh)
	if !cache.WaitForCacheSync(ctx.Done(), ps.podInformer.Informer().HasSynced, ps.nodeInformer.Informer().HasSynced) {
		klog.Errorf("failed to wait for caches to sync")
		return
	}
	// TODO: add communication interface to scheduler
}

// BuildGetPodsAssignedToNodeFunc establishes an indexer to map the pods and their assigned nodes.
// It returns a function to help us get all the pods that assigned to a node based on the indexer.
func (ps *PredictorServer) BuildGetPodsAssignedToNodeFunc(podInformer informerv1.PodInformer) (GetPodsAssignedToNodeFunc, error) {
	// Establish an indexer to map the pods and their assigned nodes.
	err := podInformer.Informer().AddIndexers(cache.Indexers{
		nodeNameKeyIndex: func(obj interface{}) ([]string, error) {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				return []string{}, nil
			}
			if len(pod.Spec.NodeName) == 0 {
				return []string{}, nil
			}
			return []string{pod.Spec.NodeName}, nil
		},
	})
	if err != nil {
		return nil, err
	}

	// The indexer helps us get all the pods that assigned to a node.
	podIndexer := podInformer.Informer().GetIndexer()
	getPodsAssignedToNode := func(nodeName string) ([]*corev1.Pod, error) {
		objs, err := podIndexer.ByIndex(nodeNameKeyIndex, nodeName)
		if err != nil {
			return nil, err
		}
		pods := make([]*corev1.Pod, 0, len(objs))
		for _, obj := range objs {
			pod, ok := obj.(*corev1.Pod)
			if !ok {
				continue
			}
			pods = append(pods, pod)
		}
		return pods, nil
	}
	return getPodsAssignedToNode, nil
}

func (ps *PredictorServer) MaxAcceptableReplicas(ctx context.Context, requirements appsapi.ReplicaRequirements) (map[string]int32, error) {

	defaultAcceptableReplicas := map[string]int32{
		framework.DefaultAcceptableReplicasKey: 0,
	}

	nodes, err := ps.nodeLister.List(labels.SelectorFromSet(requirements.NodeSelector))
	if err != nil {
		return defaultAcceptableReplicas, fmt.Errorf("failed to get nodes that match node selector, err: %v", err)
	}
	nodeInfoList := make([]*framework.NodeInfo, len(nodes))
	var nodesLen int32
	getNodeInfo := func(i int) {
		pods, err := ps.getPodFunc(nodes[i].Name)
		if err != nil {
			klog.V(6).InfoS("failed to get pods in nodes", "nodes", nodes[i].Name)
		} else {
			nodeInfo := framework.NewNodeInfo(nodes[i], pods)
			length := atomic.AddInt32(&nodesLen, 1)
			nodeInfoList[length-1] = nodeInfo
		}
	}
	ps.framework.Parallelizer().Until(ctx, len(nodes), getNodeInfo)

	// Step 1: Filter Nodes.
	feasibleNodes, err := findNodesThatFitRequirements(ctx, ps.framework, &requirements, nodeInfoList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	if len(feasibleNodes) == 0 {
		return defaultAcceptableReplicas, fmt.Errorf("no feasible nodes found")
	}

	// Step 2: cal max available replicas for each feasibleNodes.
	nodeScoreList, err := computeReplicas(ctx, ps.framework, &requirements, feasibleNodes)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	// Step 3: Prioritize clusters.
	priorityList, err := prioritizeNodes(ctx, ps.framework, &requirements, feasibleNodes, nodeScoreList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}

	// step4 aggregate the max available replicas
	result, err := aggregateReplicas(ctx, ps.framework, &requirements, priorityList)
	if err != nil {
		return defaultAcceptableReplicas, err
	}
	return result, nil
}

func (ps *PredictorServer) UnschedulableReplicas(ctx context.Context, gvk metav1.GroupVersionKind, namespacedName string,
	labelSelector map[string]string) (int32, error) {
	// TODO: add real logic
	return 0, nil
}
