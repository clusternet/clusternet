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

package tainttoleration

import (
	"context"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/generated/clientset/versioned/fake"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
)

func clusterWithTaints(clusterNamespace string, taints []v1.Taint) *clusterapi.ManagedCluster {
	return &clusterapi.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cluster-test",
			Namespace: clusterNamespace,
		},
		Spec: clusterapi.ManagedClusterSpec{
			Taints: taints,
		},
	}
}

func subscriptionWithTolerations(subName string, tolerations []v1.Toleration) *appsapi.Subscription {
	return &appsapi.Subscription{
		ObjectMeta: metav1.ObjectMeta{
			Name:      subName,
			Namespace: "default",
		},
		Spec: appsapi.SubscriptionSpec{
			Subscribers: []appsapi.Subscriber{
				{
					ClusterAffinity: &metav1.LabelSelector{MatchLabels: map[string]string{}},
				},
			},
			ClusterTolerations: tolerations,
		},
	}
}

func TestTaintTolerationScore(t *testing.T) {
	tests := []struct {
		name         string
		subscription *appsapi.Subscription
		clusters     []*clusterapi.ManagedCluster
		expectedList framework.ClusterScoreList
	}{
		// basic test case
		{
			name: "cluster with taints tolerated by the subscription, gets a higher score than those cluster with intolerable taints",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{
					{
						Key:      "foo",
						Operator: v1.TolerationOpEqual,
						Value:    "bar",
						Effect:   v1.TaintEffectPreferNoSchedule,
					},
				},
			),
			clusters: []*clusterapi.ManagedCluster{
				clusterWithTaints(
					"cluster-ns-01",
					[]v1.Taint{
						{
							Key:    "foo",
							Value:  "bar",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
				clusterWithTaints(
					"cluster-ns-02",
					[]v1.Taint{
						{
							Key:    "foo",
							Value:  "blah",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
			},
			expectedList: []framework.ClusterScore{
				{NamespacedName: "cluster-ns-01/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-02/cluster-test", Score: 0},
			},
		},
		// the count of taints that are tolerated by subscription, does not matter.
		{
			name: "the clusters that all of their taints are tolerated by the subscription, get the same score, no matter how many tolerable taints a cluster has",
			subscription: subscriptionWithTolerations("sub1", []v1.Toleration{
				{
					Key:      "cpu-type",
					Operator: v1.TolerationOpEqual,
					Value:    "arm64",
					Effect:   v1.TaintEffectPreferNoSchedule,
				},
				{
					Key:      "disk-type",
					Operator: v1.TolerationOpEqual,
					Value:    "ssd",
					Effect:   v1.TaintEffectPreferNoSchedule,
				},
			}),
			clusters: []*clusterapi.ManagedCluster{
				clusterWithTaints("cluster-ns-01", []v1.Taint{}),
				clusterWithTaints(
					"cluster-ns-02",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
				clusterWithTaints(
					"cluster-ns-03",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						}, {
							Key:    "disk-type",
							Value:  "ssd",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
			},
			expectedList: []framework.ClusterScore{
				{NamespacedName: "cluster-ns-01/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-02/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-03/cluster-test", Score: framework.MaxClusterScore},
			},
		},
		// the count of taints on a cluster that are not tolerated by subscription, matters.
		{
			name: "the more intolerable taints a cluster has, the lower score it gets.",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{
					{
						Key:      "foo",
						Operator: v1.TolerationOpEqual,
						Value:    "bar",
						Effect:   v1.TaintEffectPreferNoSchedule,
					},
				},
			),
			clusters: []*clusterapi.ManagedCluster{
				clusterWithTaints("cluster-ns-01", []v1.Taint{}),
				clusterWithTaints(
					"cluster-ns-02",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
				clusterWithTaints(
					"cluster-ns-03",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						}, {
							Key:    "disk-type",
							Value:  "ssd",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
			},
			expectedList: []framework.ClusterScore{
				{NamespacedName: "cluster-ns-01/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-02/cluster-test", Score: 50},
				{NamespacedName: "cluster-ns-03/cluster-test", Score: 0},
			},
		},
		// taints-tolerations priority only takes care about the taints and tolerations that have effect PreferNoSchedule
		{
			name: "only taints and tolerations that have effect PreferNoSchedule are checked by taints-tolerations priority function",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{
					{
						Key:      "cpu-type",
						Operator: v1.TolerationOpEqual,
						Value:    "arm64",
						Effect:   v1.TaintEffectNoSchedule,
					}, {
						Key:      "disk-type",
						Operator: v1.TolerationOpEqual,
						Value:    "ssd",
						Effect:   v1.TaintEffectNoSchedule,
					},
				},
			),
			clusters: []*clusterapi.ManagedCluster{
				clusterWithTaints("cluster-ns-01", []v1.Taint{}),
				clusterWithTaints(
					"cluster-ns-02",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectNoSchedule,
						},
					},
				),
				clusterWithTaints(
					"cluster-ns-03",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						}, {
							Key:    "disk-type",
							Value:  "ssd",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
			},
			expectedList: []framework.ClusterScore{
				{NamespacedName: "cluster-ns-01/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-02/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-03/cluster-test", Score: 0},
			},
		},
		{
			name: "Default behaviour No taints and tolerations, lands on cluster with no taints",
			// subscription without tolerations
			subscription: subscriptionWithTolerations("sub2", []v1.Toleration{}),
			clusters: []*clusterapi.ManagedCluster{
				// Cluster without taints
				clusterWithTaints("cluster-ns-01", []v1.Taint{}),
				clusterWithTaints(
					"cluster-ns-02",
					[]v1.Taint{
						{
							Key:    "cpu-type",
							Value:  "arm64",
							Effect: v1.TaintEffectPreferNoSchedule,
						},
					},
				),
			},
			expectedList: []framework.ClusterScore{
				{NamespacedName: "cluster-ns-01/cluster-test", Score: framework.MaxClusterScore},
				{NamespacedName: "cluster-ns-02/cluster-test", Score: 0},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fakeInformerFactory := informers.NewSharedInformerFactory(&fake.Clientset{}, 0*time.Second)
			for _, cls := range test.clusters {
				if err := fakeInformerFactory.Clusters().V1beta1().ManagedClusters().Informer().GetStore().Add(cls); err != nil {
					t.Fatal(err)
				}
			}

			fh, err := runtime.NewFramework(nil, nil, runtime.WithInformerFactory(fakeInformerFactory))
			if err != nil {
				t.Fatal(err)
			}

			p, _ := New(nil, fh)
			var gotList framework.ClusterScoreList
			for _, n := range test.clusters {
				score, status := p.(framework.ScorePlugin).Score(context.Background(), nil, test.subscription, klog.KObj(n).String())
				if !status.IsSuccess() {
					t.Errorf("unexpected error: %v", status)
				}
				gotList = append(gotList, framework.ClusterScore{NamespacedName: klog.KObj(n).String(), Score: score})
			}

			status := p.(framework.ScorePlugin).ScoreExtensions().NormalizeScore(context.Background(), nil, nil, gotList)
			if !status.IsSuccess() {
				t.Errorf("unexpected error: %v", status)
			}

			if !reflect.DeepEqual(test.expectedList, gotList) {
				t.Errorf("expected:\n\t%+v,\ngot:\n\t%+v", test.expectedList, gotList)
			}
		})
	}
}

func TestTaintTolerationFilter(t *testing.T) {
	tests := []struct {
		name         string
		subscription *appsapi.Subscription
		cluster      *clusterapi.ManagedCluster
		wantStatus   *framework.Status
	}{
		{
			name:         "A subscription having no tolerations can't be scheduled onto a cluster with nonempty taints",
			subscription: subscriptionWithTolerations("sub1", []v1.Toleration{}),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}},
			),
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable,
				"clusters(s) had taint {dedicated: user1}, that the subscription didn't tolerate"),
		},
		{
			name: "A subscription which can be scheduled on a dedicated cluster assigned to user1 with effect NoSchedule",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}},
			),
		},
		{
			name: "A subscription which can't be scheduled on a dedicated cluster assigned to user2 with effect NoSchedule",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "dedicated", Value: "user1", Effect: "NoSchedule"}},
			),
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable,
				"clusters(s) had taint {dedicated: user1}, that the subscription didn't tolerate"),
		},
		{
			name: "A subscription can be scheduled onto the cluster, with a toleration uses operator Exists that tolerates the taints on the cluster",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "foo", Operator: "Exists", Effect: "NoSchedule"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}},
			),
		},
		{
			name: "A subscription has multiple tolerations, cluster has multiple taints, all the taints are tolerated, subscription can be scheduled onto the cluster",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{
					{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"},
					{Key: "foo", Operator: "Exists", Effect: "NoSchedule"},
				},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{
					{Key: "dedicated", Value: "user2", Effect: "NoSchedule"},
					{Key: "foo", Value: "bar", Effect: "NoSchedule"},
				},
			),
		},
		{
			name: "A subscription has a toleration that keys and values match the taint on the cluster, but (non-empty) effect doesn't match, " +
				"can't be scheduled onto the cluster",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "foo", Operator: "Equal", Value: "bar", Effect: "PreferNoSchedule"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}},
			),
			wantStatus: framework.NewStatus(framework.UnschedulableAndUnresolvable,
				"clusters(s) had taint {foo: bar}, that the subscription didn't tolerate"),
		},
		{
			name: "The subscription has a toleration that keys and values match the taint on the cluster, the effect of toleration is empty, " +
				"and the effect of taint is NoSchedule. Subscription can be scheduled onto the cluster",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "foo", Operator: "Equal", Value: "bar"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "foo", Value: "bar", Effect: "NoSchedule"}},
			),
		},
		{
			name: "The subscription has a toleration that key and value don't match the taint on the cluster, " +
				"but the effect of taint on cluster is PreferNoSchedule. Subscription can be scheduled onto the cluster",
			subscription: subscriptionWithTolerations(
				"sub1",
				[]v1.Toleration{{Key: "dedicated", Operator: "Equal", Value: "user2", Effect: "NoSchedule"}},
			),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "dedicated", Value: "user1", Effect: "PreferNoSchedule"}},
			),
		},
		{
			name: "The subscription has no toleration, " +
				"but the effect of taint on cluster is PreferNoSchedule. Subscription can be scheduled onto the cluster",
			subscription: subscriptionWithTolerations("sub1", []v1.Toleration{}),
			cluster: clusterWithTaints(
				"clusterA",
				[]v1.Taint{{Key: "dedicated", Value: "user1", Effect: "PreferNoSchedule"}},
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p, _ := New(nil, nil)
			gotStatus := p.(framework.FilterPlugin).Filter(context.Background(), nil, test.subscription, test.cluster)
			if !reflect.DeepEqual(gotStatus, test.wantStatus) {
				t.Errorf("status does not match: %v, want: %v", gotStatus, test.wantStatus)
			}
		})
	}
}
