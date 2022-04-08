package dividing

import (
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/utils"
)

type weightList struct {
	weightSum int64
	weights   []int64
}

// StaticDivideReplicas will fill the target replicas of all feeds based on a weight list and feed finv.
func StaticDivideReplicas(selected *framework.TargetClusters, sub *appsapi.Subscription, clusters []*clusterapi.ManagedCluster, finv *appsapi.FeedInventory) error {
	wl := getWeightList(sub.Spec.Subscribers, clusters)
	if wl.weightSum == 0 {
		return nil
	}
	if selected.Replicas == nil {
		selected.Replicas = make(map[string][]int32)
	}
	for _, feed := range sub.Spec.Feeds {
		var order *appsapi.FeedOrder
		for i := range finv.Spec.Feeds {
			if feed == finv.Spec.Feeds[i].Feed {
				order = &finv.Spec.Feeds[i]
				break
			}
		}
		if order != nil && order.DesiredReplicas != nil {
			replicas := staticDivideReplicas(*order.DesiredReplicas, &wl)
			selected.Replicas[utils.GetFeedKey(&order.Feed)] = replicas
		} else {
			selected.Replicas[utils.GetFeedKey(&order.Feed)] = []int32{}
		}
	}
	return nil
}

// staticDivideReplicas divides replicas by the weight list.
func staticDivideReplicas(desiredReplicas int32, wl *weightList) []int32 {
	res := make([]int32, len(wl.weights))
	remain := desiredReplicas
	for i, weight := range wl.weights {
		replica := weight * int64(desiredReplicas) / wl.weightSum
		res[i] = int32(replica)
		remain -= int32(replica)
	}
	if remain == 0 {
		return res
	}

	clusterIndices := sortByWeight(wl.weights)
	for i := 0; i < int(remain); i++ {
		res[clusterIndices[i]]++
	}
	return res
}

// sortByWeight sorts the cluster index based on their weight
func sortByWeight(weights []int64) []int {
	clusterIndices := make([]int, len(weights))
	for i := range weights {
		clusterIndices[i] = i
	}
	sort.Slice(clusterIndices, func(i, j int) bool {
		return weights[clusterIndices[i]] > weights[clusterIndices[j]]
	})
	return clusterIndices
}

// getWeightList analyzes weight from subscribers and form a weightList based on the sequence of clusters.
func getWeightList(subscribers []appsapi.Subscriber, clusters []*clusterapi.ManagedCluster) weightList {
	res := weightList{
		weights: make([]int64, 0, len(clusters)),
	}
	for _, cluster := range clusters {
		var clusterWeightSum int64
		for _, subscriber := range subscribers {
			if subscriber.Weight <= 0 {
				continue
			}
			selector, err := metav1.LabelSelectorAsSelector(subscriber.ClusterAffinity)
			if err != nil {
				continue
			}
			if !selector.Matches(labels.Set(cluster.Labels)) {
				continue
			}
			clusterWeightSum += int64(subscriber.Weight)
			res.weightSum += int64(subscriber.Weight)
		}
		res.weights = append(res.weights, clusterWeightSum)
	}
	return res
}
