/*
Copyright 2023 The Clusternet Authors.

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

package runtime

import (
	"reflect"
	"testing"

	utilpointer "k8s.io/utils/pointer"

	framework "github.com/clusternet/clusternet/pkg/scheduler/framework/interfaces"
)

func TestMergeFeedReplicas(t *testing.T) {

	type args struct {
		a framework.FeedReplicas
		b framework.FeedReplicas
	}
	tests := []struct {
		name string
		args args
		want framework.FeedReplicas
	}{
		{
			name: "both a and b are empty",
			args: args{
				a: make(framework.FeedReplicas, 5),
				b: make(framework.FeedReplicas, 5),
			},
			want: make(framework.FeedReplicas, 5),
		},
		{
			name: "a is empty",
			args: args{
				a: make(framework.FeedReplicas, 3),
				b: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), utilpointer.Int32(4)},
			},
			want: make(framework.FeedReplicas, 3),
		},
		{
			name: "b is empty",
			args: args{
				a: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), utilpointer.Int32(4)},
				b: make(framework.FeedReplicas, 3),
			},
			want: make(framework.FeedReplicas, 3),
		},
		{
			name: "a is shorter",
			args: args{
				a: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(5)},
				b: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), utilpointer.Int32(4)},
			},
			want: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(3), utilpointer.Int32(4)},
		},
		{
			name: "b is shorter",
			args: args{
				a: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), utilpointer.Int32(4)},
				b: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(5)},
			},
			want: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(3), utilpointer.Int32(4)},
		},
		{
			name: "a and b have different values",
			args: args{
				a: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), utilpointer.Int32(4)},
				b: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(5), utilpointer.Int32(1)},
			},
			want: framework.FeedReplicas{utilpointer.Int32(1), utilpointer.Int32(3), utilpointer.Int32(1)},
		},
		{
			name: "a and b have partial nil values",
			args: args{
				a: framework.FeedReplicas{utilpointer.Int32(2), utilpointer.Int32(3), nil},
				b: framework.FeedReplicas{utilpointer.Int32(1), nil, utilpointer.Int32(1)},
			},
			want: framework.FeedReplicas{utilpointer.Int32(1), nil, nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mergeFeedReplicas(tt.args.a, tt.args.b); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("mergeFeedReplicas() = %v, want %v", got, tt.want)
			}
		})
	}
}
