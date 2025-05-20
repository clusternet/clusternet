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

package algorithm

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	clusterapi "github.com/clusternet/clusternet/pkg/apis/clusters/v1beta1"
	"github.com/clusternet/clusternet/pkg/scheduler/cache"
	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
)

type scheduleCache struct{}

// NumClusters returns the number of clusters in the cache.
func (s *scheduleCache) NumClusters() int {
	clusters, err := s.List(&metav1.LabelSelector{})
	if err != nil {
		return 0
	}
	return len(clusters)
}

// List returns the list of clusters in the cache.
func (s *scheduleCache) List(_ *metav1.LabelSelector) ([]*clusterapi.ManagedCluster, error) {
	return nil, nil
}

// Get returns the ManagedCluster of the given cluster.
func (s *scheduleCache) Get(namespacedName string) (*clusterapi.ManagedCluster, error) {
	if namespacedName == "clusternet-123/cluster1" {
		return &clusterapi.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"region": "ap-beijing"}},
		}, nil
	}
	if namespacedName == "clusternet-456/cluster2" {
		return &clusterapi.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"region": "ap-beijing"}},
		}, nil
	}
	if namespacedName == "clusternet-789/cluster3" {
		return &clusterapi.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"region": "ap-shanghai"}},
		}, nil
	}
	return nil, nil
}

func TestSubgroupClusters(t *testing.T) {
	type fields struct {
		cache                       cache.Cache
		percentageOfClustersToScore int32
		nextStartClusterIndex       int
	}
	type args struct {
		sub              *appsapi.Subscription
		clusterScoreList framework.ClusterScoreList
	}

	schedulingBySubGroup := true
	scheduleCacheObj := &scheduleCache{}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    framework.ClusterScoreList
		wantErr bool
	}{
		// Add test cases.
		{
			name: "test1",
			fields: fields{
				cache: scheduleCacheObj,
			},
			args: args{
				sub: &appsapi.Subscription{
					Spec: appsapi.SubscriptionSpec{
						SchedulingBySubGroup: &schedulingBySubGroup,
						Subscribers: []appsapi.Subscriber{
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"region": "ap-beijing",
									},
								},
								SubGroupStrategy: &appsapi.SubGroupStrategy{
									MinClusters: 1,
								},
							},
							{
								ClusterAffinity: &metav1.LabelSelector{
									MatchLabels: map[string]string{
										"region": "ap-shanghai",
									},
								},
								SubGroupStrategy: &appsapi.SubGroupStrategy{
									MinClusters: 1,
								},
							},
						},
					},
				},
				clusterScoreList: []framework.ClusterScore{
					{
						NamespacedName: "clusternet-123/cluster1",
						Score:          99,
					},
					{
						NamespacedName: "clusternet-456/cluster2",
						Score:          97,
					},
					{
						NamespacedName: "clusternet-789/cluster3",
						Score:          88,
					},
				},
			},
			want: []framework.ClusterScore{
				{
					NamespacedName: "clusternet-123/cluster1",
					Score:          99,
				},
				{
					NamespacedName: "clusternet-789/cluster3",
					Score:          88,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &genericScheduler{
				cache:                       tt.fields.cache,
				percentageOfClustersToScore: tt.fields.percentageOfClustersToScore,
				nextStartClusterIndex:       tt.fields.nextStartClusterIndex,
			}
			got, err := g.subgroupClusters(tt.args.sub, tt.args.clusterScoreList)
			if (err != nil) != tt.wantErr {
				t.Errorf("subgroupClusters() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			fmt.Println("got: ", got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("subgroupClusters() got = %v, want %v", got, tt.want)
			}
		})
	}
}
