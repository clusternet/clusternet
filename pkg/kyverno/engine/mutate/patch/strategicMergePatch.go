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
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	"sigs.k8s.io/kustomize/api/filters/patchstrategicmerge"
	filtersutil "sigs.k8s.io/kustomize/kyaml/filtersutil"
	yaml "sigs.k8s.io/kustomize/kyaml/yaml"
)

// ProcessStrategicMergePatch ...
func ProcessStrategicMergePatch(ruleName string, overlay interface{}, resource unstructured.Unstructured) (resp response.RuleResponse, patchedResource unstructured.Unstructured) {
	startTime := time.Now()
	klog.V(4).Info("started applying strategicMerge patch", "startTime", startTime)
	resp.Name = ruleName
	resp.Type = response.Mutation

	defer func() {
		resp.RuleStats.ProcessingTime = time.Since(startTime)
		resp.RuleStats.RuleExecutionTimestamp = startTime.Unix()
		klog.V(4).Info("finished applying strategicMerge patch", "processingTime", resp.RuleStats.ProcessingTime.String())
	}()

	overlayBytes, err := json.Marshal(overlay)
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "failed to marshal resource")
		resp.Message = fmt.Sprintf("failed to process patchStrategicMerge: %v", err)
		return resp, resource
	}

	base, err := json.Marshal(resource.Object)
	if err != nil {
		resp.Status = response.RuleStatusFail
		klog.Error(err, "failed to marshal resource")
		resp.Message = fmt.Sprintf("failed to process patchStrategicMerge: %v", err)
		return resp, resource
	}
	patchedBytes, err := strategicMergePatch(string(base), string(overlayBytes))
	if err != nil {
		klog.Error(err, "failed to apply patchStrategicMerge")
		msg := fmt.Sprintf("failed to apply patchStrategicMerge: %v", err)
		resp.Status = response.RuleStatusFail
		resp.Message = msg
		return resp, resource
	}

	err = patchedResource.UnmarshalJSON(patchedBytes)
	if err != nil {
		klog.Error(err, "failed to unmarshal resource")
		resp.Status = response.RuleStatusFail
		resp.Message = fmt.Sprintf("failed to process patchStrategicMerge: %v", err)
		return resp, resource
	}

	klog.V(6).Info("generating JSON patches from patched resource", "patchedResource", patchedResource.Object)

	jsonPatches, err := generatePatches(base, patchedBytes)
	if err != nil {
		msg := fmt.Sprintf("failed to generated JSON patches from patched resource: %v", err.Error())
		resp.Status = response.RuleStatusFail
		klog.V(2).Info(msg)
		resp.Message = msg
		return resp, patchedResource
	}

	for _, p := range jsonPatches {
		klog.V(5).Info("generated patch", "patch", string(p))
	}

	resp.Status = response.RuleStatusPass
	resp.Patches = jsonPatches
	resp.Message = "applied strategic merge patch"
	return resp, patchedResource
}

func strategicMergePatch(base, overlay string) ([]byte, error) {
	preprocessedYaml, err := preProcessStrategicMergePatch(overlay, base)
	if err != nil {
		_, isConditionError := err.(ConditionError)
		_, isGlobalConditionError := err.(GlobalConditionError)

		if isConditionError || isGlobalConditionError {
			if err = preprocessedYaml.UnmarshalJSON([]byte(`{}`)); err != nil {
				return []byte{}, err
			}
		} else {
			return []byte{}, fmt.Errorf("failed to preProcess rule: %+v", err)
		}
	}

	patchStr, _ := preprocessedYaml.String()
	klog.V(3).Info("applying strategic merge patch", "patch", patchStr)
	f := patchstrategicmerge.Filter{
		Patch: preprocessedYaml,
	}

	baseObj := buffer{Buffer: bytes.NewBufferString(base)}
	err = filtersutil.ApplyToJSON(f, baseObj)

	return baseObj.Bytes(), err
}

func preProcessStrategicMergePatch(pattern, resource string) (*yaml.RNode, error) {
	patternNode := yaml.MustParse(pattern)
	resourceNode := yaml.MustParse(resource)

	err := preProcessPattern(patternNode, resourceNode)

	return patternNode, err
}
