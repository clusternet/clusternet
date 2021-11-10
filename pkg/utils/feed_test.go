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
	"reflect"
	"testing"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

func TestFormatFeed(t *testing.T) {
	tests := []struct {
		name string
		feed appsapi.Feed
		want string
	}{
		{
			feed: appsapi.Feed{
				Kind:       "Guess",
				APIVersion: "v1",
				Namespace:  "",
				Name:       "what",
			},
			want: "Guess what",
		},
		{
			feed: appsapi.Feed{
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

func TestFindObsoletedFeeds(t *testing.T) {
	tests := []struct {
		name     string
		oldFeeds []appsapi.Feed
		newFeeds []appsapi.Feed
		want     []appsapi.Feed
	}{
		{
			name: "same feeds",
			oldFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
			},
			newFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
			},
			want: []appsapi.Feed{},
		},

		{
			name: "same feeds, but order differs",
			oldFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
			},
			newFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
			},
			want: []appsapi.Feed{},
		},

		{
			name: "feeds differed",
			oldFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
			},
			newFeeds: []appsapi.Feed{
				{
					Kind:      "Abc",
					Namespace: "foo",
					Name:      "bar",
				},
				{
					Kind:      "Ghi",
					Namespace: "demo",
					Name:      "test",
				},
				{
					Kind:      "Jkl",
					Namespace: "fly",
					Name:      "high",
				},
			},
			want: []appsapi.Feed{
				{
					Kind:      "Def",
					Namespace: "far",
					Name:      "away",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FindObsoletedFeeds(tt.oldFeeds, tt.newFeeds); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindObsoletedFeeds() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasFeed(t *testing.T) {
	tests := []struct {
		name  string
		feed  appsapi.Feed
		feeds []appsapi.Feed
		want  bool
	}{
		{
			name: "not found",
			feed: appsapi.Feed{
				Kind:      "ABC",
				Namespace: "def",
				Name:      "foo",
			},
			feeds: []appsapi.Feed{
				{
					Kind:       "DEF",
					APIVersion: "v1",
					Namespace:  "def",
					Name:       "foo",
				},
				{
					Kind:       "GHI",
					APIVersion: "v1",
					Namespace:  "def",
					Name:       "far",
				},
			},
			want: false,
		},

		{
			name: "found",
			feed: appsapi.Feed{
				Kind:      "ABC",
				Namespace: "def",
				Name:      "foo",
			},
			feeds: []appsapi.Feed{
				{
					Kind:       "DEF",
					APIVersion: "v1",
					Namespace:  "def",
					Name:       "foo",
				},
				{
					Kind:       "ABC",
					APIVersion: "v1",
					Namespace:  "def",
					Name:       "foo",
				},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasFeed(tt.feed, tt.feeds); got != tt.want {
				t.Errorf("HasFeed() = %v, want %v", got, tt.want)
			}
		})
	}
}
