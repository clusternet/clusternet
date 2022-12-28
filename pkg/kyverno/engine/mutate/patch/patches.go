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

package patch

import (
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

// Patcher patches the resource
type Patcher interface {
	Patch() (resp response.RuleResponse, newPatchedResource unstructured.Unstructured)
}

// patchStrategicMergeHandler
type patchStrategicMergeHandler struct {
	ruleName        string
	patch           apiextensions.JSON
	patchedResource unstructured.Unstructured
	evalCtx         context.EvalInterface
}

func NewPatchStrategicMerge(ruleName string, patch apiextensions.JSON, patchedResource unstructured.Unstructured, context context.EvalInterface) Patcher {
	return patchStrategicMergeHandler{
		ruleName:        ruleName,
		patch:           patch,
		patchedResource: patchedResource,
		evalCtx:         context,
	}
}

func (h patchStrategicMergeHandler) Patch() (response.RuleResponse, unstructured.Unstructured) {
	return ProcessStrategicMergePatch(h.ruleName, h.patch, h.patchedResource)
}

// patchesJSON6902Handler
type patchesJSON6902Handler struct {
	ruleName        string
	patches         string
	patchedResource unstructured.Unstructured
}

func NewPatchesJSON6902(ruleName string, patches string, patchedResource unstructured.Unstructured) Patcher {
	return patchesJSON6902Handler{
		ruleName:        ruleName,
		patches:         patches,
		patchedResource: patchedResource,
	}
}

func (h patchesJSON6902Handler) Patch() (resp response.RuleResponse, patchedResource unstructured.Unstructured) {
	resp.Name = h.ruleName
	resp.Type = response.Mutation

	patchesJSON6902, err := ConvertPatchesToJSON(h.patches)
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "error in type conversion")
		resp.Message = err.Error()
		return resp, unstructured.Unstructured{}
	}

	return ProcessPatchJSON6902(h.ruleName, patchesJSON6902, h.patchedResource)
}
