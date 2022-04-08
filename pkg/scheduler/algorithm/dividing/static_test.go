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

package dividing

import (
	"reflect"
	"testing"
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
