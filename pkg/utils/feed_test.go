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

package utils

import (
	"testing"

	"github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func TestFormatFeed(t *testing.T) {
	tests := []struct {
		name string
		feed v1alpha1.Feed
		want string
	}{
		{
			feed: v1alpha1.Feed{
				Kind:       "Guess",
				APIVersion: "v1",
				Namespace:  "",
				Name:       "what",
			},
			want: "Guess what",
		},
		{
			feed: v1alpha1.Feed{
				Kind:       "Guess",
				APIVersion: "v1",
				Namespace:  "demo",
				Name:       "what",
			},
			want: "Guess demo/what",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatFeed(tt.feed); got != tt.want {
				t.Errorf("FormatFeed() = %v, want %v", got, tt.want)
			}
		})
	}
}
