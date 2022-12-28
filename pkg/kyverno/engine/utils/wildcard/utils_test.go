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

package wildcard

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContainsWildcard(t *testing.T) {
	type args struct {
		v string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{{
		name: "no wildcard",
		args: args{
			v: "name",
		},
		want: false,
	}, {
		name: "empty string",
		args: args{
			v: "",
		},
		want: false,
	}, {
		name: "contains * at the end",
		args: args{
			v: "name*",
		},
		want: true,
	}, {
		name: "contains * at the beginning",
		args: args{
			v: "*name",
		},
		want: true,
	}, {
		name: "contains * in the middle",
		args: args{
			v: "start*end",
		},
		want: true,
	}, {
		name: "only *",
		args: args{
			v: "*",
		},
		want: true,
	}, {
		name: "contains ? at the end",
		args: args{
			v: "name?",
		},
		want: true,
	}, {
		name: "contains ? at the beginning",
		args: args{
			v: "?name",
		},
		want: true,
	}, {
		name: "contains ? in the middle",
		args: args{
			v: "start?end",
		},
		want: true,
	}, {
		name: "only ?",
		args: args{
			v: "?",
		},
		want: true,
	}, {
		name: "both * and ?",
		args: args{
			v: "*name?",
		},
		want: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ContainsWildcard(tt.args.v))
		})
	}
}
