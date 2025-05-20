/*
Copyright 2021 The Clusternet Authors.

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

package agent

import (
	"strings"
	"testing"
)

func TestGenerateClusterName(t *testing.T) {
	for _, tt := range []struct {
		name              string
		clusterName       string
		clusterNamePrefix string
		wanted            string
	}{
		{
			name:              "valid cluster name",
			clusterName:       "abc",
			clusterNamePrefix: "foo",
			wanted:            "abc",
		},
		{
			name:              "empty cluster name",
			clusterName:       "",
			clusterNamePrefix: "foo",
			wanted:            "foo-12345", // this is a fake name, but it has the same length.
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := generateClusterName(tt.clusterName, tt.clusterNamePrefix)
			if len(tt.clusterName) > 0 {
				if got != tt.wanted {
					t.Errorf("generateClusterName() = %v, want %v", got, tt.wanted)
				}
			} else {
				if len(got) != len(tt.wanted) || !strings.HasPrefix(got, tt.clusterNamePrefix+"-") {
					t.Errorf("wrong generate name")
				}
			}
		})
	}
}
