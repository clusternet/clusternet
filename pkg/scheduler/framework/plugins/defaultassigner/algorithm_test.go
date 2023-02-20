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

package defaultassigner

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/generated/clientset/versioned/fake"
	informers "github.com/clusternet/clusternet/pkg/generated/informers/externalversions"
	"github.com/clusternet/clusternet/pkg/known"
	schedulercache "github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/scheduler/framework/runtime"
	"github.com/clusternet/clusternet/pkg/utils"
)

func TestSortByWeight(t *testing.T) {
	tests := []struct {
		name    string
		weights []int64
		want    []int
	}{
		{
			weights: []int64{3, 1, 2},
			want:    []int{0, 2, 1},
		},
		{
			weights: []int64{1, 2, 3, 4},
			want:    []int{3, 2, 1, 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sortByWeight(tt.weights); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("sortByWeight() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStaticDivideReplicas(t *testing.T) {
	tests := []struct {
		name            string
		desiredReplicas int32
		wl              *weightList
		want            []int32
	}{
		{
			name:            "exact division",
			desiredReplicas: 5,
			wl: &weightList{
				weightSum: 5,
				weights:   []int64{2, 1, 0, 2},
			},
			want: []int32{2, 1, 0, 2},
		},
		{
			name:            "exact division (2 times)",
			desiredReplicas: 10,
			wl: &weightList{
				weightSum: 5,
				weights:   []int64{2, 1, 2},
			},
			want: []int32{4, 2, 4},
		},
		{
			name:            "differential division - 1",
			desiredReplicas: 6,
			wl: &weightList{
				weightSum: 5,
				weights:   []int64{2, 1, 0, 2},
			},
			want: []int32{3, 1, 0, 2},
		},
		{
			name:            "differential division -2 ",
			desiredReplicas: 5,
			wl: &weightList{
				weightSum: 8,
				weights:   []int64{5, 1, 0, 2},
			},
			want: []int32{4, 0, 0, 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := staticDivideReplicas(tt.desiredReplicas, tt.wl); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("staticDivideReplicas() = %v, want %v", got, tt.want)
			}
		})
	}
}

func newClusterWithLabels(name, namespace string, labels map[string]string) *clusterapi.ManagedCluster {
	return &clusterapi.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}
}

func newDeploymentFeed(name string) appsapi.Feed {
	return appsapi.Feed{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
		Namespace:  "default",
		Name:       name,
	}
}

func newServiceFeed(name string) appsapi.Feed {
	return appsapi.Feed{
		Kind:       "Service",
		APIVersion: "v1",
		Namespace:  "default",
		Name:       name,
	}
}

func TestDivideReplicas(t *testing.T) {
	desiredReplicas := int32(12)
	type args struct {
		selected *framework.TargetClusters
		sub      *appsapi.Subscription
		finv     *appsapi.FeedInventory
	}
	tests := []struct {
		name         string
		args         args
		wantReplicas map[string][]int32
		wantErr      bool
	}{
		{
			name: "1 cluster",
			args: args{
				selected: &framework.TargetClusters{
					BindingClusters: []string{"cluster-ns-01/member1"},
				},
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
						DividingScheduling: &appsapi.DividingScheduling{Type: appsapi.StaticReplicaDividingType},
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"a": "1",
									},
								},
								Weight: 1,
							},
						},
						Feeds: []appsapi.Feed{newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
					}},
				},
			},
			wantReplicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("example-1")): {desiredReplicas},
			},
			wantErr: false,
		},
		{
			name: "2 clusters",
			args: args{
				selected: &framework.TargetClusters{
					BindingClusters: []string{"cluster-ns-01/member1", "cluster-ns-01/member2"},
				},
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
						DividingScheduling: &appsapi.DividingScheduling{Type: appsapi.StaticReplicaDividingType},
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"a": "1",
									},
								},
								Weight: 1,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "2",
									},
								},
								Weight: 2,
							},
						},
						Feeds: []appsapi.Feed{newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
					}},
				},
			},
			wantReplicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("example-1")): {4, 8},
			},
			wantErr: false,
		},
		{
			name: "3 clusters",
			args: args{
				selected: &framework.TargetClusters{
					BindingClusters: []string{"cluster-ns-01/member1", "cluster-ns-01/member2", "cluster-ns-02/member3"},
				},
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
						DividingScheduling: &appsapi.DividingScheduling{Type: appsapi.StaticReplicaDividingType},
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"a": "1",
									},
								},
								Weight: 1,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "2",
									},
								},
								Weight: 2,
							},
						},
						Feeds: []appsapi.Feed{newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
					}},
				},
			},
			wantReplicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("example-1")): {3, 6, 3},
			},
			wantErr: false,
		},
		{
			name: "3 clusters with duplicated weight",
			args: args{
				selected: &framework.TargetClusters{
					BindingClusters: []string{"cluster-ns-01/member1", "cluster-ns-01/member2", "cluster-ns-02/member3"},
				},
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
						DividingScheduling: &appsapi.DividingScheduling{Type: appsapi.StaticReplicaDividingType},
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"a": "1",
									},
								},
								Weight: 1,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "2",
									},
								},
								Weight: 0,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "1",
									},
								},
								Weight: 2,
							},
						},
						Feeds: []appsapi.Feed{newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
					}},
				},
			},
			wantReplicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("example-1")): {3, 0, 9},
			},
			wantErr: false,
		},
		{
			name: "2 feeds, 3 clusters with duplicated weight",
			args: args{
				selected: &framework.TargetClusters{
					BindingClusters: []string{"cluster-ns-01/member1", "cluster-ns-01/member2", "cluster-ns-02/member3"},
				},
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
						DividingScheduling: &appsapi.DividingScheduling{Type: appsapi.StaticReplicaDividingType},
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"a": "1",
									},
								},
								Weight: 1,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "2",
									},
								},
								Weight: 0,
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"b": "1",
									},
								},
								Weight: 2,
							},
						},
						Feeds: []appsapi.Feed{newDeploymentFeed("example-1"), newDeploymentFeed("example-2")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
						{
							Feed:            newDeploymentFeed("example-2"),
							DesiredReplicas: &desiredReplicas,
						},
					}},
				},
			},
			wantReplicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("example-1")): {3, 0, 9},
				utils.GetFeedKey(newDeploymentFeed("example-2")): {3, 0, 9},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			clusternetClient := fake.NewSimpleClientset(
				newClusterWithLabels("member1", "cluster-ns-01", map[string]string{"a": "1"}),
				newClusterWithLabels("member2", "cluster-ns-01", map[string]string{"b": "2"}),
				newClusterWithLabels("member3", "cluster-ns-02", map[string]string{"a": "1", "b": "1"}),
			)
			clusternetInformerFactory := informers.NewSharedInformerFactory(clusternetClient, known.DefaultResync)
			clusterInformer := clusternetInformerFactory.Clusters().V1beta1().ManagedClusters()

			fwk, _ := runtime.NewFramework(nil, nil, runtime.WithCache(schedulercache.New(clusterInformer.Lister())))
			assigner, err := NewStaticAssigner(nil, fwk)
			if err != nil {
				t.Fatalf("Creating plugin error: %v", err)
			}

			clusternetInformerFactory.Start(ctx.Done())
			if !cache.WaitForCacheSync(ctx.Done(), clusterInformer.Informer().HasSynced) {
				t.Fatalf("Assign() error = %v, wantErr %v", fmt.Errorf("failed to wait for cache sync"), tt.wantErr)
			}

			result, status := assigner.(framework.AssignPlugin).Assign(ctx, nil, tt.args.sub, tt.args.finv, *tt.args.selected)
			if (status != nil) != tt.wantErr {
				t.Errorf("Assign() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(result.Replicas, tt.wantReplicas) {
				t.Errorf("Assign() gotResponse = %v, want %v", result.Replicas, tt.wantReplicas)
			}
		})
	}
}

func TestDynamicDivideReplicas(t *testing.T) {
	tests := []struct {
		name                 string
		desiredReplicas      int32
		maxAvailableReplicas []int32
		want                 []int32
	}{
		{
			desiredReplicas:      6,
			maxAvailableReplicas: []int32{3, 6, 9},
			want:                 []int32{1, 2, 3},
		},
		{
			desiredReplicas:      2,
			maxAvailableReplicas: []int32{3, 6, 9},
			want:                 []int32{0, 1, 1},
		},
		{
			desiredReplicas:      3,
			maxAvailableReplicas: []int32{3, 6, 9},
			want:                 []int32{1, 1, 1},
		},
		{
			desiredReplicas:      3,
			maxAvailableReplicas: []int32{0, 0, 0},
			want:                 []int32{0, 0, 0},
		},
		{
			desiredReplicas:      6,
			maxAvailableReplicas: []int32{1, 2, 2},
			want:                 []int32{1, 2, 2},
		},
		{
			desiredReplicas:      -6,
			maxAvailableReplicas: []int32{3, 6, 9},
			want:                 []int32{-1, -2, -3},
		},
		{
			desiredReplicas:      -2,
			maxAvailableReplicas: []int32{3, 6, 9},
			want:                 []int32{0, -1, -1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dynamicDivideReplicas(tt.desiredReplicas, tt.maxAvailableReplicas); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("dynamicDivideReplicas() = %v, want %v", got, tt.want)
			}
		})
	}
}
