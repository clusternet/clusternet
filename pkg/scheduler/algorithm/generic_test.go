package algorithm

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
	"github.com/clusternet/clusternet/pkg/utils"
)

func newClusterWithLabels(name, namespace string, labels map[string]string) *clusterapi.ManagedCluster {
	return &clusterapi.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
	}
}

func newDeploymentFeed(name string) *appsapi.Feed {
	return &appsapi.Feed{
		Kind:       "Deployment",
		APIVersion: "apps/v1",
		Namespace:  "default",
		Name:       name,
	}
}

func Test_genericScheduler_divideReplicas(t *testing.T) {
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
						DividingScheduling: &appsapi.DividingSchedulingStrategy{Type: appsapi.StaticReplicaDividingType},
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
						Feeds: []appsapi.Feed{*newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            *newDeploymentFeed("example-1"),
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
						DividingScheduling: &appsapi.DividingSchedulingStrategy{Type: appsapi.StaticReplicaDividingType},
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
						Feeds: []appsapi.Feed{*newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            *newDeploymentFeed("example-1"),
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
						DividingScheduling: &appsapi.DividingSchedulingStrategy{Type: appsapi.StaticReplicaDividingType},
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
						Feeds: []appsapi.Feed{*newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            *newDeploymentFeed("example-1"),
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
						DividingScheduling: &appsapi.DividingSchedulingStrategy{Type: appsapi.StaticReplicaDividingType},
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
						Feeds: []appsapi.Feed{*newDeploymentFeed("example-1")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            *newDeploymentFeed("example-1"),
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
						DividingScheduling: &appsapi.DividingSchedulingStrategy{Type: appsapi.StaticReplicaDividingType},
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
						Feeds: []appsapi.Feed{*newDeploymentFeed("example-1"), *newDeploymentFeed("example-2")},
					},
				},
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{Feeds: []appsapi.FeedOrder{
						{
							Feed:            *newDeploymentFeed("example-1"),
							DesiredReplicas: &desiredReplicas,
						},
						{
							Feed:            *newDeploymentFeed("example-2"),
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

			g := &genericScheduler{
				cache: schedulercache.New(clusterInformer.Lister()),
			}

			clusternetInformerFactory.Start(ctx.Done())
			if !cache.WaitForCacheSync(ctx.Done(), clusterInformer.Informer().HasSynced) {
				t.Fatalf("divideReplicas() error = %v, wantErr %v", fmt.Errorf("failed to wait for cache sync"), tt.wantErr)
			}

			if err := g.divideReplicas(tt.args.selected, tt.args.sub, tt.args.finv); (err != nil) != tt.wantErr {
				t.Errorf("divideReplicas() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(tt.args.selected.Replicas, tt.wantReplicas) {
				t.Errorf("divideReplicas() gotResponse = %v, want %v", tt.args.selected.Replicas, tt.wantReplicas)
			}

		})
	}
}
