package defaultassigner

import (
	"math"
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

// DynamicDivideReplicas will fill the target replicas of all feeds based on predictor result and deviation.
// First time scheduling is considered as a special kind of scaling. When the desired replicas in deviation
// are negative, it means we should try to scale down, otherwise we try to scale up deviation replicas.
func DynamicDivideReplicas(sub *appsapi.Subscription, deviations []appsapi.FeedOrder, availableReplicas framework.TargetClusters) (framework.TargetClusters, error) {
	var err error

	result := framework.NewTargetClusters(sub.Status.BindingClusters, sub.Status.Replicas).DeepCopy()
	strategy := sub.Spec.DividingScheduling.DynamicDividing.Strategy

	for i := range deviations {
		d := deviations[i].DesiredReplicas
		_, scheduled := sub.Status.Replicas[utils.GetFeedKey(deviations[i].Feed)]
		var r framework.TargetClusters
		switch {
		// First scheduling is considered as a special kind of scaling up.
		case !scheduled || (d != nil && *d > 0):
			switch strategy {
			case appsapi.SpreadDividingStrategy:
				r = spreadScaleUp(&deviations[i], availableReplicas)
			}
		case d != nil && *d < 0:
			switch strategy {
			case appsapi.SpreadDividingStrategy:
				r = spreadScaleDown(sub, deviations[i])
			}
		}
		if err != nil {
			return *result, err
		}
		result.MergeOneFeed(&r)
	}
	return *result, nil
}

func spreadScaleUp(d *appsapi.FeedOrder, availableReplicas framework.TargetClusters) framework.TargetClusters {
	result := framework.TargetClusters{
		BindingClusters: availableReplicas.BindingClusters,
		Replicas:        make(map[string][]int32),
	}
	feedKey := utils.GetFeedKey(d.Feed)
	if d.DesiredReplicas != nil {
		replicas := dynamicDivideReplicas(*d.DesiredReplicas, availableReplicas.Replicas[feedKey])
		result.Replicas[feedKey] = replicas
	} else {
		result.Replicas[feedKey] = []int32{}
	}
	return result
}

func spreadScaleDown(sub *appsapi.Subscription, d appsapi.FeedOrder) framework.TargetClusters {
	result := framework.TargetClusters{
		BindingClusters: sub.Status.BindingClusters,
		Replicas:        make(map[string][]int32),
	}
	feedKey := utils.GetFeedKey(d.Feed)
	if d.DesiredReplicas != nil {
		replicas := dynamicDivideReplicas(*d.DesiredReplicas, sub.Status.Replicas[feedKey])
		result.Replicas[feedKey] = replicas
	} else {
		result.Replicas[feedKey] = []int32{}
	}
	return result
}

// dynamicDivideReplicas divides replicas by the MaxAvailableReplicas
func dynamicDivideReplicas(desiredReplicas int32, maxAvailableReplicas []int32) []int32 {
	res := make([]int32, len(maxAvailableReplicas))
	weightSum := utils.SumArrayInt32(maxAvailableReplicas)

	if weightSum == 0 || desiredReplicas > weightSum {
		return maxAvailableReplicas
	}

	remain := desiredReplicas

	type cluster struct {
		index   int
		decimal float64
	}
	clusters := make([]cluster, 0, len(maxAvailableReplicas))

	for i, weight := range maxAvailableReplicas {
		replica := weight * desiredReplicas / weightSum
		res[i] = replica
		remain -= replica
		clusters = append(clusters, cluster{
			index:   i,
			decimal: math.Abs(float64(weight*desiredReplicas)/float64(weightSum) - float64(replica)),
		})
	}

	// sort the clusters by descending order of decimal part of replica
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].decimal > clusters[j].decimal
	})

	if remain > 0 {
		for i := 0; i < int(remain) && i < len(res); i++ {
			res[clusters[i].index]++
		}
	} else if remain < 0 {
		for i := 0; i < int(-remain) && i < len(res); i++ {
			res[clusters[i].index]--
		}
	}

	return res
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
			selected.Replicas[utils.GetFeedKey(order.Feed)] = replicas
		} else {
			selected.Replicas[utils.GetFeedKey(order.Feed)] = []int32{}
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
