package interfaces

import (
	"reflect"
	"testing"
)

func TestTargetClustersWithFirstScheduling_Merge(t *testing.T) {
	source := &TargetClusters{
		BindingClusters: []string{},
		Replicas:        map[string][]int32{},
	}

	tests := []struct {
		name   string
		b      *TargetClusters
		result *TargetClusters
	}{
		{
			name: "f1 and f2 in c1 and c2",
			b: &TargetClusters{
				BindingClusters: []string{"c1", "c2"},
				Replicas: map[string][]int32{
					"f1": {1, 1},
					"f2": {1, 2},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2"},
				Replicas: map[string][]int32{
					"f1": {1, 1},
					"f2": {1, 2},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t1 *testing.T) {
			tcp := source.DeepCopy()
			for k, v := range tt.b.Replicas {
				tcp.MergeOneFeed(&TargetClusters{
					BindingClusters: tt.b.BindingClusters,
					Replicas:        map[string][]int32{k: v},
				})
			}
			if !reflect.DeepEqual(tcp, tt.result) {
				t.Errorf("Merge() %s gotResponse = %v, want %v", tt.name, tcp, tt.result)
			}
		})
	}
}

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
		{
			name: "unsorted cluster",
			b: &TargetClusters{
				BindingClusters: []string{"c1", "c3", "c2"},
				Replicas: map[string][]int32{
					"f2": {1, 2, 0},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					"f1": {1, 2, 3},
					"f2": {1, 0, 2},
					"f3": {1, 0, 1},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t1 *testing.T) {
			tcp := template.DeepCopy()
			for k, v := range tt.b.Replicas {
				tcp.MergeOneFeed(&TargetClusters{
					BindingClusters: tt.b.BindingClusters,
					Replicas:        map[string][]int32{k: v},
				})
			}
			if !reflect.DeepEqual(tcp, tt.result) {
				t.Errorf("Merge() %s gotResponse = %v, want %v", tt.name, tcp, tt.result)
			}
		})
	}
}

func TestTargetClusters_MergeOneFeed(t *testing.T) {
	template := &TargetClusters{
		BindingClusters: []string{"c1", "c2", "c3"},
		Replicas: map[string][]int32{
			"f1": {1, 2, 3},
			"f2": {},
			"f3": {1, 0, 3},
		},
	}
	tests := []struct {
		name   string
		b      *TargetClusters
		result *TargetClusters
	}{
		{
			name: "merge one feed",
			b: &TargetClusters{
				BindingClusters: []string{
					"c1", "c2", "c3",
				},
				Replicas: map[string][]int32{
					"f1": {4, 4, 5},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{"c1", "c2", "c3"},
				Replicas: map[string][]int32{
					"f1": {5, 6, 8},
					"f2": {},
					"f3": {1, 0, 3},
				},
			},
		},
		{
			name: "merge not exist feed",
			b: &TargetClusters{
				BindingClusters: []string{
					"c1", "c2", "c3",
				},
				Replicas: map[string][]int32{
					"f4": {1, 1, 1},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{
					"c1", "c2", "c3",
				},
				Replicas: map[string][]int32{
					"f1": {1, 2, 3},
					"f2": {},
					"f3": {1, 0, 3},
					"f4": {1, 1, 1},
				},
			},
		},
		{
			name: "merge not exist cluster",
			b: &TargetClusters{
				BindingClusters: []string{
					"c1", "c4",
				},
				Replicas: map[string][]int32{
					"f1": {1, 1},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{
					"c1", "c2", "c3", "c4",
				},
				Replicas: map[string][]int32{
					"f1": {2, 2, 3, 1},
					"f2": {},
					"f3": {1, 0, 3, 0},
				},
			},
		},
		{
			name: "merge not exist cluster and feed",
			b: &TargetClusters{
				BindingClusters: []string{
					"c1", "c4",
				},
				Replicas: map[string][]int32{
					"f2": {1, 4},
				},
			},
			result: &TargetClusters{
				BindingClusters: []string{
					"c1", "c2", "c3", "c4",
				},
				Replicas: map[string][]int32{
					"f1": {1, 2, 3, 0},
					"f2": {1, 0, 0, 4},
					"f3": {1, 0, 3, 0},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t1 *testing.T) {
			tcopy := template.DeepCopy()
			tcopy.MergeOneFeed(tt.b)
			if !reflect.DeepEqual(tcopy, tt.result) {
				t.Errorf("MergeOneFeed() %s \ngot = %v\nwant= %v", tt.name, tcopy, tt.result)
			}
		})
	}
}

func TestTargetClusters_MergeOneFeed_TargetClusterEmpty(t *testing.T) {
	template := &TargetClusters{
		BindingClusters: nil,
		Replicas:        nil,
	}
	b := &TargetClusters{
		BindingClusters: []string{"c2", "c3"},
		Replicas: map[string][]int32{
			"f1": {1, 2},
			"f3": {1, 0},
		},
	}
	result := &TargetClusters{
		BindingClusters: []string{"c2", "c3"},
		Replicas: map[string][]int32{
			"f1": {1, 2},
			"f3": {1, 0},
		},
	}
	t.Run("panic", func(t *testing.T) {
		template.MergeOneFeed(b)
		if !reflect.DeepEqual(template, result) {
			t.Errorf("MergeOneFeed() \ngot = %v\nwant= %v", result, template)
		}
	})
}
