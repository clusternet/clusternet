package defaultassigner

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
	"github.com/clusternet/clusternet/pkg/utils"
)

var (
	subTemplate = &appsapi.Subscription{
		Spec: appsapi.SubscriptionSpec{
			SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
			DividingScheduling: &appsapi.DividingScheduling{
				Type: appsapi.DynamicReplicaDividingType,
				DynamicDividing: &appsapi.DynamicDividing{
					Strategy: appsapi.SpreadDividingStrategy,
				},
			},
			Feeds: []appsapi.Feed{
				newDeploymentFeed("f1"),
				newServiceFeed("f2"),
				newDeploymentFeed("f3"),
			},
		},
		Status: appsapi.SubscriptionStatus{
			BindingClusters: []string{"c1", "c2", "c3"},
			Replicas: map[string][]int32{
				utils.GetFeedKey(newDeploymentFeed("f1")): {1, 3, 0}, // already scheduled: 4
				utils.GetFeedKey(newServiceFeed("f2")):    {},
				utils.GetFeedKey(newDeploymentFeed("f3")): {1, 1, 1}, // already scheduled: 3
			},
		},
	}
)

func TestDynamicAssigner_Assign(t *testing.T) {
	type args struct {
		sub               *appsapi.Subscription
		finv              *appsapi.FeedInventory
		availableReplicas framework.TargetClusters
	}
	tests := []struct {
		name string
		args args
		want framework.TargetClusters
	}{
		{
			name: "scale up f1",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(8)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(3)},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c3"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 10, 10},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {3, 4, 1},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {1, 1, 1},
				},
			},
		},
		{
			name: "scale up f1 and f3",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(8)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(9)},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c3"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 20, 30},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {3, 4, 1},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {2, 3, 4},
				},
			},
		},
		{
			name: "scale down f1",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(2)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(3)},
						},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {0, 2, 0},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {1, 1, 1},
				},
			},
		},
		{
			name: "scale down f1 and f3",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(2)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(1)},
						},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {0, 2, 0},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {0, 0, 1},
				},
			},
		},
		{
			name: "scale up f1 and f3 with new candidate clusters",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(8)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(9)},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c4"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 20, 30},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3", "c4"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {3, 4, 0, 1},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {2, 3, 1, 3},
				},
			},
		},
		{
			name: "scale up f1 with new candidate clusters and scale down f3",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(8)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(1)},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c4"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 20, 30},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3", "c4"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {3, 4, 0, 1},
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {0, 0, 1, 0},
				},
			},
		},
		{
			name: "do nothing",
			args: args{
				sub: subTemplate.DeepCopy(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(4)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(3)},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c4"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 20, 30},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {1, 3, 0}, // already scheduled: 4
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {1, 1, 1}, // already scheduled: 3
				},
			},
		},
		{
			name: "add new secret",
			args: args{
				sub: func() *appsapi.Subscription {
					sub := subTemplate.DeepCopy()
					sub.Spec.Feeds = append(sub.Spec.Feeds, newServiceFeed("f4"))
					return sub
				}(),
				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{
							{Feed: newDeploymentFeed("f1"), DesiredReplicas: pointer.Int32(4)},
							{Feed: newServiceFeed("f2")},
							{Feed: newDeploymentFeed("f3"), DesiredReplicas: pointer.Int32(3)},
							{Feed: newServiceFeed("f4")},
						},
					},
				},
				availableReplicas: framework.TargetClusters{
					BindingClusters: []string{"c1", "c2", "c4"},
					Replicas: map[string][]int32{
						utils.GetFeedKey(newDeploymentFeed("f1")): {10, 10, 10},
						utils.GetFeedKey(newServiceFeed("f2")):    {},
						utils.GetFeedKey(newDeploymentFeed("f3")): {10, 20, 30},
						utils.GetFeedKey(newServiceFeed("f4")):    {},
					},
				},
			},
			want: framework.TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					utils.GetFeedKey(newDeploymentFeed("f1")): {1, 3, 0}, // already scheduled: 4
					utils.GetFeedKey(newServiceFeed("f2")):    {},
					utils.GetFeedKey(newDeploymentFeed("f3")): {1, 1, 1}, // already scheduled: 3
					utils.GetFeedKey(newServiceFeed("f4")):    {},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pl := new(DynamicAssigner)
			state := framework.NewCycleState()

			status := pl.PreAssign(context.TODO(), state, tt.args.sub, tt.args.finv, tt.args.availableReplicas)
			if status != nil {
				t.Errorf("PreAssign() got status = %v, want nil", status)
			}

			got, status := pl.Assign(context.TODO(), state, tt.args.sub, tt.args.finv, tt.args.availableReplicas)
			if status != nil {
				t.Errorf("Assign() got status = %v, want nil", status)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Assign() got = %v, want %v", got, tt.want)
			}
		})
	}
}
