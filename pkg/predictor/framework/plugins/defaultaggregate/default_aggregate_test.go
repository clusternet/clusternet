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

package defaultaggregate

import (
	"context"
	"fmt"
	"testing"

	schedulerapi "github.com/clusternet/clusternet/pkg/apis/scheduler"
	framework "github.com/clusternet/clusternet/pkg/predictor/framework/interfaces"
)

func TestDefaultAggregator(t *testing.T) {
	tests := []struct {
		scores framework.NodeScoreList
		want   int32
	}{
		{
			scores: framework.NodeScoreList{
				framework.NodeScore{
					Name:                 "node1",
					Score:                0,
					MaxAvailableReplicas: 4,
				},
				framework.NodeScore{
					Name:                 "node2",
					Score:                0,
					MaxAvailableReplicas: 10,
				},
			},
			want: 14,
		},
	}
	for i, test := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			p, _ := New(nil, nil)
			maxAvailableReplicas, status := p.(framework.AggregatePlugin).Aggregate(context.Background(), nil, test.scores)
			if got := status.AsError(); got != nil {
				t.Errorf("status does not match %q, want nil", got)
			}
			if maxAvailableReplicas[schedulerapi.DefaultAcceptableReplicasKey] != test.want {
				t.Errorf("got different maxAvailableReplicas got %q, want %q",
					maxAvailableReplicas[schedulerapi.DefaultAcceptableReplicasKey], test.want)
			}
		})
	}
}
