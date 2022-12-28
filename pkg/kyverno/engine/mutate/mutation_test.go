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

package mutate

import (
	"encoding/json"
	"testing"

	types "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/utils"
	"gotest.tools/assert"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// jsonPatch is used to build test patches
type jsonPatch struct {
	Path      string             `json:"path,omitempty" yaml:"path,omitempty"`
	Operation string             `json:"op,omitempty" yaml:"op,omitempty"`
	Value     apiextensions.JSON `json:"value,omitempty" yaml:"value,omitempty"`
}

const endpointsDocument string = `{
	"kind": "Endpoints",
	"apiVersion": "v1",
	"metadata": {
		"name": "my-endpoint-service",
		"labels": {
			"originalLabel": "isHere"
		}
	},
	"subsets": [
		{
			"addresses": [
				{
					"ip": "1.2.3.4"
				}
			],
			"ports": [
				{
					"port": 9376
				}
			]
		}
	]
}`

func applyPatches(patchConfig *types.KyvernoPatchConfig, resource unstructured.Unstructured) (*response.RuleResponse, unstructured.Unstructured) {
	mutateResp := Mutate(patchConfig, context.NewContext(), resource)

	if mutateResp.Status != response.RuleStatusPass {
		return &response.RuleResponse{
			Type:    response.Mutation,
			Status:  mutateResp.Status,
			Message: mutateResp.Message,
		}, resource
	}

	return &response.RuleResponse{
		Type:    response.Mutation,
		Status:  response.RuleStatusPass,
		Patches: mutateResp.Patches,
	}, mutateResp.PatchedResource
}

func TestProcessPatches_EmptyPatches(t *testing.T) {
	var emptyPatchConfig = &types.KyvernoPatchConfig{}
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}

	rr, _ := applyPatches(emptyPatchConfig, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusError)
	assert.Assert(t, len(rr.Patches) == 0)
}

func makeAddIsMutatedLabelPatch() jsonPatch {
	return jsonPatch{
		Path:      "/metadata/labels/is-mutated",
		Operation: "add",
		Value:     "true",
	}
}

func makeRuleWithPatch(t *testing.T, patch jsonPatch) *types.KyvernoPatchConfig {
	patches := []jsonPatch{patch}
	return makeRuleWithPatches(t, patches)
}

func makeRuleWithPatches(t *testing.T, patches []jsonPatch) *types.KyvernoPatchConfig {
	jsonPatches, err := json.Marshal(patches)
	if err != nil {
		t.Errorf("failed to marshal patch: %v", err)
	}

	mutation := types.KyvernoMutation{
		PatchesJSON6902: string(jsonPatches),
	}
	return &types.KyvernoPatchConfig{
		Mutation: mutation,
	}
}

func TestProcessPatches_EmptyDocument(t *testing.T) {
	rule := makeRuleWithPatch(t, makeAddIsMutatedLabelPatch())
	rr, _ := applyPatches(rule, unstructured.Unstructured{})
	assert.Equal(t, rr.Status, response.RuleStatusFail)
	assert.Assert(t, len(rr.Patches) == 0)
}

func TestProcessPatches_AllEmpty(t *testing.T) {
	emptyRule := &types.KyvernoPatchConfig{}
	rr, _ := applyPatches(emptyRule, unstructured.Unstructured{})
	assert.Equal(t, rr.Status, response.RuleStatusError)
	assert.Assert(t, len(rr.Patches) == 0)
}

func TestProcessPatches_AddPathDoesntExist(t *testing.T) {
	patch := makeAddIsMutatedLabelPatch()
	patch.Path = "/metadata/additional/is-mutated"
	rule := makeRuleWithPatch(t, patch)
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}
	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusSkip)
	assert.Assert(t, len(rr.Patches) == 0)
}

func TestProcessPatches_RemovePathDoesntExist(t *testing.T) {
	patch := jsonPatch{Path: "/metadata/labels/is-mutated", Operation: "remove"}
	rule := makeRuleWithPatch(t, patch)
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}
	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusSkip)
	assert.Assert(t, len(rr.Patches) == 0)
}

func TestProcessPatches_AddAndRemovePathsDontExist_EmptyResult(t *testing.T) {
	patch1 := jsonPatch{Path: "/metadata/labels/is-mutated", Operation: "remove"}
	patch2 := jsonPatch{Path: "/spec/labels/label3", Operation: "add", Value: "label3Value"}
	rule := makeRuleWithPatches(t, []jsonPatch{patch1, patch2})
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}
	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusPass)
	assert.Equal(t, len(rr.Patches), 1)
}

func TestProcessPatches_AddAndRemovePathsDontExist_ContinueOnError_NotEmptyResult(t *testing.T) {
	patch1 := jsonPatch{Path: "/metadata/labels/is-mutated", Operation: "remove"}
	patch2 := jsonPatch{Path: "/spec/labels/label2", Operation: "remove", Value: "label2Value"}
	patch3 := jsonPatch{Path: "/metadata/labels/label3", Operation: "add", Value: "label3Value"}
	rule := makeRuleWithPatches(t, []jsonPatch{patch1, patch2, patch3})
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}

	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusPass)
	assert.Assert(t, len(rr.Patches) != 0)
	assertEqStringAndData(t, `{"path":"/metadata/labels/label3","op":"add","value":"label3Value"}`, rr.Patches[0])
}

func TestProcessPatches_RemovePathDoesntExist_EmptyResult(t *testing.T) {
	patch := jsonPatch{Path: "/metadata/labels/is-mutated", Operation: "remove"}
	rule := makeRuleWithPatch(t, patch)
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}
	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusSkip)
	assert.Assert(t, len(rr.Patches) == 0)
}

func TestProcessPatches_RemovePathDoesntExist_NotEmptyResult(t *testing.T) {
	patch1 := jsonPatch{Path: "/metadata/labels/is-mutated", Operation: "remove"}
	patch2 := jsonPatch{Path: "/metadata/labels/label2", Operation: "add", Value: "label2Value"}
	rule := makeRuleWithPatches(t, []jsonPatch{patch1, patch2})
	resourceUnstructured, err := utils.ConvertToUnstructured([]byte(endpointsDocument))
	if err != nil {
		t.Error(err)
	}
	rr, _ := applyPatches(rule, *resourceUnstructured)
	assert.Equal(t, rr.Status, response.RuleStatusPass)
	assert.Assert(t, len(rr.Patches) == 1)
	assertEqStringAndData(t, `{"path":"/metadata/labels/label2","op":"add","value":"label2Value"}`, rr.Patches[0])
}

func assertEqStringAndData(t *testing.T, str string, data []byte) {
	var p1 jsonPatch
	json.Unmarshal([]byte(str), &p1)

	var p2 jsonPatch
	json.Unmarshal([]byte(data), &p2)

	assert.Equal(t, p1, p2)
}
