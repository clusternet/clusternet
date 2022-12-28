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
	"fmt"

	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/mutate/patch"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/utils"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/variables"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Response struct {
	Status          response.RuleStatus
	PatchedResource unstructured.Unstructured
	Patches         [][]byte
	Message         string
}

func newErrorResponse(msg string, err error) *Response {
	return newResponse(response.RuleStatusError, unstructured.Unstructured{}, nil, fmt.Sprintf("%s: %v", msg, err))
}

func newResponse(status response.RuleStatus, resource unstructured.Unstructured, patches [][]byte, msg string) *Response {
	return &Response{
		Status:          status,
		PatchedResource: resource,
		Patches:         patches,
		Message:         msg,
	}
}

func Mutate(patchConfig *kyvernov1.KyvernoPatchConfig, ctx context.Interface, resource unstructured.Unstructured) *Response {
	if patchConfig == nil {
		return newErrorResponse("patch config cannot be empty", nil)
	}
	updatedPatchConfig, err := variables.SubstituteAllInPatchConfig(ctx, *patchConfig)
	if err != nil {
		return newErrorResponse("variable substitution failed", err)
	}

	m := updatedPatchConfig.Mutation
	patcher := NewPatcher("", m.GetPatchStrategicMerge(), m.PatchesJSON6902, resource, ctx)
	if patcher == nil {
		return newResponse(response.RuleStatusError, resource, nil, "empty mutate rule")
	}

	resp, patchedResource := patcher.Patch()
	if resp.Status != response.RuleStatusPass {
		return newResponse(resp.Status, resource, nil, resp.Message)
	}

	if resp.Patches == nil {
		return newResponse(response.RuleStatusSkip, resource, nil, "no patches applied")
	}

	if err := ctx.AddResource(patchedResource.Object); err != nil {
		return newErrorResponse("failed to update patched resource in the JSON context", err)
	}

	return newResponse(response.RuleStatusPass, patchedResource, resp.Patches, resp.Message)
}

func ForEach(name string, foreach kyvernov1.KyvernoForEachMutation, ctx context.Interface, resource unstructured.Unstructured) *Response {
	fe, err := substituteAllInForEach(foreach, ctx)
	if err != nil {
		return newErrorResponse("variable substitution failed", err)
	}

	patcher := NewPatcher(name, fe.GetPatchStrategicMerge(), fe.PatchesJSON6902, resource, ctx)
	if patcher == nil {
		return newResponse(response.RuleStatusError, unstructured.Unstructured{}, nil, "no patches found")
	}

	resp, patchedResource := patcher.Patch()
	if resp.Status != response.RuleStatusPass {
		return newResponse(resp.Status, unstructured.Unstructured{}, nil, resp.Message)
	}

	if resp.Patches == nil {
		return newResponse(response.RuleStatusSkip, unstructured.Unstructured{}, nil, "no patches applied")
	}

	if err := ctx.AddResource(patchedResource.Object); err != nil {
		return newErrorResponse("failed to update patched resource in the JSON context", err)
	}

	return newResponse(response.RuleStatusPass, patchedResource, resp.Patches, resp.Message)
}

func substituteAllInForEach(fe kyvernov1.KyvernoForEachMutation, ctx context.Interface) (*kyvernov1.KyvernoForEachMutation, error) {
	jsonObj, err := utils.ToMap(fe)
	if err != nil {
		return nil, err
	}

	data, err := variables.SubstituteAll(ctx, jsonObj)
	if err != nil {
		return nil, err
	}

	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var updatedForEach kyvernov1.KyvernoForEachMutation
	if err := json.Unmarshal(bytes, &updatedForEach); err != nil {
		return nil, err
	}

	return &updatedForEach, nil
}

func NewPatcher(name string, strategicMergePatch apiextensions.JSON, jsonPatch string, r unstructured.Unstructured, ctx context.Interface) patch.Patcher {
	if strategicMergePatch != nil {
		return patch.NewPatchStrategicMerge(name, strategicMergePatch, r, ctx)
	}

	if len(jsonPatch) > 0 {
		return patch.NewPatchesJSON6902(name, jsonPatch, r)
	}

	return nil
}
