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

package options

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

func TestValidateServiceAccountToken(t *testing.T) {
	var tests = []struct {
		token    string
		expected bool
	}{
		{"eyJhbGciOiJSUzI1NiIsImtpZCI6IjQzTDdTVDBLb0Q2OUlOTTN3LUx5M1VHMVZmbUFTVVgyV1NtSHRDSGstcTgifQ.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJjbHVzdGVybmV0LXN5c3RlbSIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VjcmV0Lm5hbWUiOiJjbHVzdGVyLWJvb3RzdHJhcC11c2UiLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoiY2x1c3Rlci1ib290c3RyYXAtdXNlIiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiNDJhZTAwMzgtYWE1Yy00ZWZiLThmNzQtYzA2MTg2ZTM3YjYyIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmNsdXN0ZXJuZXQtc3lzdGVtOmNsdXN0ZXItYm9vdHN0cmFwLXVzZSJ9.p6M2r4yOYrVvd8z2kz1TGHzHXwj18KU1GCljSZY2hom5iJNvsliNK9Z1N8vNOAU-P0s-2cqQolZYVQKn1clrHgGlfNmGtKIFSZtCpF0N-11nUkp-mQAjQazC3IaeQtx1db6WNsLDm3jllR85xTdlezSp7VzsDWw81hfXKjEG-ZljqJLV5fFi8Wp2tq3riDy4dWIt_PG89TNsvvDKbJsYcuqhWZsg0qC_qjzIluRoMZFbl7-Xm_Teid-9l6mzKCxXpqD6mlbis50C-wJUYeu_NGgf5Xco9Cx6ol6n73fYApY7_Q7smH6JAg8G_0b5hrbgkfO4u0e6uvdCBrYZ0xXB0Q", true},
		{"07401b.23424.23423", true},
		{"07401b.f395accd246ae2d", false},
		{"07401b.f395accd246ae52d@foobar", false},
	}
	for _, bt := range tests {
		if validateServiceAccountTokenRegex.MatchString(bt.token) != bt.expected {
			t.Errorf("invalid serviceAccount token %q", bt.token)
		}
	}
}
