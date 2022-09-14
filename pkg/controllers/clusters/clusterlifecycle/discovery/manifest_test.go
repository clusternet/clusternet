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

package discovery

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestClusternetAgentDeployment(t *testing.T) {
	deployValues := struct {
		Namespace string
		Image     string
		Token     string
		ParentURL string
	}{
		Namespace: "clusternet-system",
		Image:     "ghcr.io/clusternet/clusternet-agent:v0.12.0",
		Token:     "sample",
		ParentURL: "https://some.k8s.com:6443/",
	}
	expectedResult := strings.ReplaceAll(ClusternetAgentDeployment, "{{ .Namespace }}", deployValues.Namespace)
	expectedResult = strings.ReplaceAll(expectedResult, "{{ .Image }}", deployValues.Image)
	expectedResult = strings.ReplaceAll(expectedResult, "{{ .Token }}", deployValues.Token)
	expectedResult = strings.ReplaceAll(expectedResult, "{{ .ParentURL }}", deployValues.ParentURL)

	result, err := parseManifest("template-clusternet-agent-deployment", ClusternetAgentDeployment, deployValues)
	if err != nil {
		t.Error(err)
	}

	if result != expectedResult {
		t.Errorf("did not match. \n %s ", cmp.Diff(expectedResult, result))
	}
}

func TestClusternetAgentRBACRules(t *testing.T) {
	rbacValues := struct {
		Namespace string
	}{
		Namespace: "clusternet-system",
	}
	expectedResult := strings.ReplaceAll(ClusternetAgentRBACRules, "{{ .Namespace }}", rbacValues.Namespace)

	result, err := parseManifest("template-clusternet-agent-rbac", ClusternetAgentRBACRules, rbacValues)
	if err != nil {
		t.Error(err)
	}

	if result != expectedResult {
		t.Errorf("did not match. \n %s ", cmp.Diff(expectedResult, result))
	}
}
