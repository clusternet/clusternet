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

package agent

import (
	"testing"

	bootstraputil "k8s.io/cluster-bootstrap/token/util"
)

func TestValidateToken(t *testing.T) {
	var tests = []struct {
		token    string
		expected bool
	}{
		{"07401b.f395accd246ae52d", true},
		{".f395accd246ae52d", false},
		{"07401b.", false},
		{"07401b.f395accd246ae52d@foobar", false},
	}
	for _, bt := range tests {
		if bootstraputil.IsValidBootstrapToken(bt.token) != bt.expected {
			t.Errorf("invalid bootstrap token %q", bt.token)
		}
	}
}
