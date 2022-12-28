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
	"fmt"
	"time"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// ProcessPatchJSON6902 ...
func ProcessPatchJSON6902(ruleName string, patchesJSON6902 []byte, resource unstructured.Unstructured) (resp response.RuleResponse, patchedResource unstructured.Unstructured) {
	startTime := time.Now()
	klog.V(4).Info("started JSON6902 patch", "startTime", startTime)
	resp.Name = ruleName
	resp.Type = response.Mutation
	defer func() {
		resp.RuleStats.ProcessingTime = time.Since(startTime)
		resp.RuleStats.RuleExecutionTimestamp = startTime.Unix()
		klog.V(4).Info("applied JSON6902 patch", "processingTime", resp.RuleStats.ProcessingTime.String())
	}()

	resourceRaw, err := resource.MarshalJSON()
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "failed to marshal resource")
		resp.Message = fmt.Sprintf("failed to marshal resource: %v", err)
		return resp, resource
	}

	patchedResourceRaw, err := applyPatchesWithOptions(resourceRaw, patchesJSON6902)
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "failed to apply JSON Patch")
		resp.Message = fmt.Sprintf("failed to apply JSON Patch: %v", err)
		return resp, resource
	}

	patchesBytes, err := generatePatches(resourceRaw, patchedResourceRaw)
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "unable generate patch bytes from base and patched document, apply patchesJSON6902 directly")
		resp.Message = fmt.Sprintf("unable generate patch bytes from base and patched document, apply patchesJSON6902 directly: %v", err)
		return resp, resource
	}

	for _, p := range patchesBytes {
		klog.V(4).Info("generated JSON Patch (RFC 6902)", "patch", string(p))
	}

	err = patchedResource.UnmarshalJSON(patchedResourceRaw)
	if err != nil {
		klog.Error(err, "failed to unmarshal resource")
		resp.Status = response.RuleStatusFail
		resp.Message = fmt.Sprintf("failed to unmarshal resource: %v", err)
		return resp, resource
	}

	resp.Status = response.RuleStatusPass
	resp.Message = string("applied JSON Patch")
	resp.Patches = patchesBytes
	return resp, patchedResource
}

func applyPatchesWithOptions(resource, patch []byte) ([]byte, error) {
	patches, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		return resource, fmt.Errorf("failed to decode patches: %v", err)
	}

	options := &jsonpatch.ApplyOptions{SupportNegativeIndices: true, AllowMissingPathOnRemove: true, EnsurePathExistsOnAdd: true}
	patchedResource, err := patches.ApplyWithOptions(resource, options)
	if err != nil {
		return resource, err
	}

	return patchedResource, nil
}

// ConvertPatchesToJSON ...
func ConvertPatchesToJSON(patchesJSON6902 string) ([]byte, error) {
	if len(patchesJSON6902) == 0 {
		return []byte(patchesJSON6902), nil
	}

	if patchesJSON6902[0] != '[' {
		// If the patch doesn't look like a JSON6902 patch, we
		// try to parse it to json.
		op, err := yaml.YAMLToJSON([]byte(patchesJSON6902))
		if err != nil {
			return nil, fmt.Errorf("failed to convert patchesJSON6902 to JSON: %v", err)
		}
		return op, nil
	}

	return []byte(patchesJSON6902), nil
}
