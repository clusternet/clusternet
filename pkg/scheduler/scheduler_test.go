package scheduler

import (
	"testing"

	"k8s.io/utils/pointer"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/utils"
)

func TestIsFeedChanged(t *testing.T) {
	feeds := []appsapi.Feed{
		{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
			Namespace:  "default",
			Name:       "deploy",
		},
		{
			Kind:       "StatefulSet",
			APIVersion: "apps/v1",
			Namespace:  "default",
			Name:       "sts",
		},
		{
			Kind:       "ConfigMap",
			APIVersion: "v1",
			Namespace:  "default",
			Name:       "cm",
		},
	}
	type args struct {
		replicas   map[string][]int32
		feedOrders []appsapi.FeedOrder
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "same replicas",
			args: args{
				replicas: map[string][]int32{
					utils.GetFeedKey(feeds[0]): {1, 2},
					utils.GetFeedKey(feeds[1]): {1, 1},
					utils.GetFeedKey(feeds[2]): {},
				},
				feedOrders: []appsapi.FeedOrder{
					{
						Feed:            feeds[0],
						DesiredReplicas: pointer.Int32(3),
					},
					{
						Feed:            feeds[1],
						DesiredReplicas: pointer.Int32(2),
					},
					{
						Feed:            feeds[2],
						DesiredReplicas: nil,
					},
				},
			},
			want: false,
		},
		{
			name: "scale up",
			args: args{
				replicas: map[string][]int32{
					utils.GetFeedKey(feeds[0]): {1, 2},
					utils.GetFeedKey(feeds[1]): {1, 1},
					utils.GetFeedKey(feeds[2]): {},
				},
				feedOrders: []appsapi.FeedOrder{
					{
						Feed:            feeds[0],
						DesiredReplicas: pointer.Int32(5),
					},
					{
						Feed:            feeds[1],
						DesiredReplicas: pointer.Int32(2),
					},
					{
						Feed:            feeds[2],
						DesiredReplicas: nil,
					},
				},
			},
			want: true,
		},
		{
			name: "scale down",
			args: args{
				replicas: map[string][]int32{
					utils.GetFeedKey(feeds[0]): {1, 2},
					utils.GetFeedKey(feeds[1]): {1, 1},
					utils.GetFeedKey(feeds[2]): {},
				},
				feedOrders: []appsapi.FeedOrder{
					{
						Feed:            feeds[0],
						DesiredReplicas: pointer.Int32(2),
					},
					{
						Feed:            feeds[1],
						DesiredReplicas: pointer.Int32(1),
					},
					{
						Feed:            feeds[2],
						DesiredReplicas: nil,
					},
				},
			},
			want: true,
		},
		{
			name: "new feed",
			args: args{
				replicas: map[string][]int32{
					utils.GetFeedKey(feeds[0]): {1, 2},
					utils.GetFeedKey(feeds[2]): {},
				},
				feedOrders: []appsapi.FeedOrder{
					{
						Feed:            feeds[0],
						DesiredReplicas: pointer.Int32(3),
					},
					{
						Feed:            feeds[1],
						DesiredReplicas: pointer.Int32(1),
					},
					{
						Feed:            feeds[2],
						DesiredReplicas: nil,
					},
				},
			},
			want: true,
		},
		{
			name: "delete feed",
			args: args{
				replicas: map[string][]int32{
					utils.GetFeedKey(feeds[0]): {1, 2},
					utils.GetFeedKey(feeds[1]): {1, 1},
				},
				feedOrders: []appsapi.FeedOrder{
					{
						Feed:            feeds[0],
						DesiredReplicas: pointer.Int32(2),
					},
					{
						Feed:            feeds[1],
						DesiredReplicas: pointer.Int32(2),
					},
					{
						Feed:            feeds[2],
						DesiredReplicas: nil,
					},
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFeedChanged(&appsapi.Subscription{
				Spec:   appsapi.SubscriptionSpec{Feeds: feeds},
				Status: appsapi.SubscriptionStatus{Replicas: tt.args.replicas},
			}, &appsapi.FeedInventory{
				Spec: appsapi.FeedInventorySpec{Feeds: tt.args.feedOrders},
			}); got != tt.want {
				t.Errorf("isFeedChanged() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_Admint(t *testing.T) {
	feeds := []appsapi.Feed{
		{
			Kind:       "Deployment",
			APIVersion: "apps/v1",
			Namespace:  "default",
			Name:       "deploy",
		},
	}

	type args struct {
		subs *appsapi.Subscription
		finv *appsapi.FeedInventory
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "only change sub spec",
			args: args{
				subs: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						DividingScheduling: &appsapi.DividingScheduling{
							Type: appsapi.DynamicReplicaDividingType,
							DynamicDividing: &appsapi.DynamicDividing{
								Strategy: appsapi.SpreadDividingStrategy,
							},
						},
						Feeds:              feeds,
						SchedulerName:      "default",
						SchedulingStrategy: appsapi.DividingSchedulingStrategyType,
					},
					Status: appsapi.SubscriptionStatus{
						BindingClusters: []string{"cluster1"},
						Replicas:        map[string][]int32{utils.GetFeedKey(feeds[0]): {4}},
						SpecHash:        0000000001,
					},
				},

				finv: &appsapi.FeedInventory{
					Spec: appsapi.FeedInventorySpec{
						Feeds: []appsapi.FeedOrder{{
							Feed:            feeds[0],
							DesiredReplicas: pointer.Int32(2),
						}},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := admit(tt.args.subs, tt.args.finv); got != tt.want {
				t.Errorf("test %s want %v, but got %v", tt.name, tt.want, got)
			}
		})
	}

}
