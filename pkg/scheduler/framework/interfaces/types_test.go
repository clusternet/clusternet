package interfaces

import (
	"reflect"
	"testing"
)

func TestTargetClusters_Merge(t *testing.T) {
	template := &TargetClusters{
		BindingClusters: []string{"c1", "c2", "c3"},
		Replicas: map[string][]int32{
			"f1": {1, 2, 3},
			"f2": {},
			"f3": {1, 0, 1},
		},
	}
	tests := []struct {
		name   string
		b      *TargetClusters
		result *TargetClusters
	}{
		{
			name: "f1 and f3 in c2 and c3",
			b: &TargetClusters{
				BindingClusters: []string{"c2", "c3"},
				Replicas: map[string][]int32{
					"f1": {1, 2},
					"f3": {1, 0},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					"f1": {1, 3, 5},
					"f2": {},
					"f3": {1, 1, 1},
				},
			},
		},
		{
			name: "f1 and f3 in c2 and c4",
			b: &TargetClusters{
				BindingClusters: []string{"c2", "c4"},
				Replicas: map[string][]int32{
					"f1": {1, 2},
					"f3": {1, 2},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3", "c4"},
				Replicas: map[string][]int32{
					"f1": {1, 3, 3, 2},
					"f2": {},
					"f3": {1, 1, 1, 2},
				},
			},
		},
		{
			name: "f3, f4 and f5 in c2 and c4",
			b: &TargetClusters{
				BindingClusters: []string{"c2", "c4"},
				Replicas: map[string][]int32{
					"f3": {1, 2},
					"f4": {1, 2},
					"f5": {2, 3},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3", "c4"},
				Replicas: map[string][]int32{
					"f1": {1, 2, 3, 0},
					"f2": {},
					"f3": {1, 1, 1, 2},
					"f4": {0, 1, 0, 2},
					"f5": {0, 2, 0, 3},
				},
			},
		},
		{
			name: "f3, f4 and f5 in c0 and c4",
			b: &TargetClusters{
				BindingClusters: []string{"c0", "c4"},
				Replicas: map[string][]int32{
					"f3": {1, 2},
					"f4": {1, 2},
					"f5": {},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3", "c0", "c4"},
				Replicas: map[string][]int32{
					"f1": {1, 2, 3, 0, 0},
					"f2": {},
					"f3": {1, 0, 1, 1, 2},
					"f4": {0, 0, 0, 1, 2},
					"f5": {},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t1 *testing.T) {
			tcp := template.DeepCopy()
			tcp.Merge(tt.b)
			if !reflect.DeepEqual(tcp, tt.result) {
				t.Errorf("Merge() gotResponse = %v, want %v", tcp, tt.result)
			}
		})
	}
}
