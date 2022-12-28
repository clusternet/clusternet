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
	"encoding/json"
	"fmt"
	"regexp"

	v1alpha1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	commonAnchor "github.com/clusternet/clusternet/pkg/kyverno/engine/anchor"
	jsonutils "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/json"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

// ApplyPatches patches given resource with given patches and returns patched document
// return original resource if any error occurs
func ApplyPatches(resource []byte, patches [][]byte) ([]byte, error) {
	if len(patches) == 0 {
		return resource, nil
	}
	joinedPatches := jsonutils.JoinPatches(patches...)
	patch, err := jsonpatch.DecodePatch(joinedPatches)
	if err != nil {
		klog.V(5).Info("failed to decode JSON patch", "patch", patch)
		return resource, err
	}

	patchedDocument, err := patch.Apply(resource)
	if err != nil {
		klog.V(5).Info("failed to apply JSON patch", "patch", patch)
		return resource, err
	}

	klog.V(5).Info("applied JSON patch", "patch", patch)
	return patchedDocument, err
}

// ApplyPatchNew patches given resource with given joined patches
func ApplyPatchNew(resource, patch []byte) ([]byte, error) {
	jsonpatch, err := jsonpatch.DecodePatch(patch)
	if err != nil {
		return resource, err
	}

	patchedResource, err := jsonpatch.Apply(resource)
	if err != nil {
		return resource, err
	}

	return patchedResource, err
}

// ConvertToUnstructured converts the resource to unstructured format
func ConvertToUnstructured(data []byte) (*unstructured.Unstructured, error) {
	resource := &unstructured.Unstructured{}
	err := resource.UnmarshalJSON(data)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

// GetAnchorsFromMap gets the conditional anchor map
func GetAnchorsFromMap(anchorsMap map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for key, value := range anchorsMap {
		if commonAnchor.IsConditionAnchor(key) {
			result[key] = value
		}
	}

	return result
}

var regexVersion = regexp.MustCompile(`v(\d+).(\d+).(\d+)\.*`)

// CopyMap creates a full copy of the target map
func CopyMap(m map[string]interface{}) map[string]interface{} {
	mapCopy := make(map[string]interface{})
	for k, v := range m {
		mapCopy[k] = v
	}

	return mapCopy
}

// CopySlice create a full copy of the target slice
func CopySlice(m []interface{}) []interface{} {
	var mListCopy []interface{}
	mListCopy = append(mListCopy, m...)
	return mListCopy
}

// CopySliceOfMaps creates a full copy of the target slice
func CopySliceOfMaps(s []map[string]interface{}) []interface{} {
	sliceCopy := make([]interface{}, len(s))
	for i, v := range s {
		sliceCopy[i] = CopyMap(v)
	}

	return sliceCopy
}

func ToMap(data interface{}) (map[string]interface{}, error) {
	if m, ok := data.(map[string]interface{}); ok {
		return m, nil
	}

	b, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	mapData := make(map[string]interface{})
	err = json.Unmarshal(b, &mapData)
	if err != nil {
		return nil, err
	}

	return mapData, nil
}

// Contains checks if a string is contained in a list of string
func contains(list []string, element string, fn func(string, string) bool) bool {
	for _, e := range list {
		if fn(e, element) {
			return true
		}
	}
	return false
}

// ApiextensionsJsonToKyvernoConditions takes in user-provided conditions in abstract apiextensions.JSON form
// and converts it into []kyverno.Condition or kyverno.AnyAllConditions according to its content.
// it also helps in validating the condtions as it returns an error when the conditions are provided wrongfully by the user.
func ApiextensionsJsonToKyvernoConditions(original apiextensions.JSON) (interface{}, error) {
	path := "preconditions/validate.deny.conditions"

	// checks for the existence any other field apart from 'any'/'all' under preconditions/validate.deny.conditions
	unknownFieldChecker := func(jsonByteArr []byte, path string) error {
		allowedKeys := map[string]bool{
			"any": true,
			"all": true,
		}
		var jsonDecoded map[string]interface{}
		if err := json.Unmarshal(jsonByteArr, &jsonDecoded); err != nil {
			return fmt.Errorf("error occurred while checking for unknown fields under %s: %+v", path, err)
		}
		for k := range jsonDecoded {
			if !allowedKeys[k] {
				return fmt.Errorf("unknown field '%s' found under %s", k, path)
			}
		}
		return nil
	}

	// marshalling the abstract apiextensions.JSON back to JSON form
	jsonByte, err := json.Marshal(original)
	if err != nil {
		return nil, fmt.Errorf("error occurred while marshalling %s: %+v", path, err)
	}

	var kyvernoOldConditions []v1alpha1.KyvernoCondition
	if err = json.Unmarshal(jsonByte, &kyvernoOldConditions); err == nil {
		var validConditionOperator bool

		for _, jsonOp := range kyvernoOldConditions {
			for _, validOp := range v1alpha1.ConditionOperators {
				if jsonOp.Operator == validOp {
					validConditionOperator = true
				}
			}
			if !validConditionOperator {
				return nil, fmt.Errorf("invalid condition operator: %s", jsonOp.Operator)
			}
			validConditionOperator = false
		}

		return kyvernoOldConditions, nil
	}

	var kyvernoAnyAllConditions v1alpha1.AnyAllConditions
	if err = json.Unmarshal(jsonByte, &kyvernoAnyAllConditions); err == nil {
		// checking if unknown fields exist or not
		err = unknownFieldChecker(jsonByte, path)
		if err != nil {
			return nil, fmt.Errorf("error occurred while parsing %s: %+v", path, err)
		}
		return kyvernoAnyAllConditions, nil
	}
	return nil, fmt.Errorf("error occurred while parsing %s: %+v", path, err)
}
