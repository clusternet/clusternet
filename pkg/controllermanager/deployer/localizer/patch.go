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

package localizer

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	jsoniter "github.com/json-iterator/go"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// maximum number of operations a single json patch may contain.
	maxJSONPatchOperations = 10000
	// nonIndent is the non-indented format for json patch.
	nonIndent = ""
	// jsonIndent is the indented format for json patch.
	jsonIndent = "    "
)

// applyOverrides applies the overrides to the current resource.
// `genericResult` the generic result of the overrides.
// `chartResult` the helmchart result after applying the overrides.
func applyOverrides(genericOriginal []byte, chartOriginal []byte, overrides []appsapi.OverrideConfig) ([]byte, []byte, error) {
	overrides = append(overrides, defaultOverrideConfigs...)
	genericResult, chartResult := genericOriginal, chartOriginal
	for _, overrideConfig := range overrides {
		// validates override value first
		if len(strings.TrimSpace(overrideConfig.Value)) == 0 {
			continue
		}
		overrideBytes, err := yaml.YAMLToJSON([]byte(overrideConfig.Value))
		if err != nil {
			return nil, nil, fmt.Errorf("failed to convert patch %s to JSON: %v", overrideConfig.Value, err)
		}

		if overrideConfig.OverrideChart {
			if len(strings.TrimSpace(string(chartResult))) == 0 {
				chartResult = overrideBytes
				continue
			}
		} else {
			if len(strings.TrimSpace(string(genericResult))) == 0 && overrideConfig.Type != appsapi.JSONPatchType {
				genericResult = overrideBytes
				continue
			}
		}

		switch overrideConfig.Type {
		case appsapi.HelmType:
			if !overrideConfig.OverrideChart {
				genericResult, err = applyHelmValuesOverride(genericResult, overrideBytes)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
				}
				break
			}

			chartResult, err = jsonpatch.MergePatch(chartResult, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.JSONPatchType:
			genericResult, err = applyJSONPatch(genericResult, overrideBytes, nonIndent)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.MergePatchType:
			genericResult, err = jsonpatch.MergePatch(genericResult, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.FieldJSONPatchType:
			genericResult, err = applyFieldJSONPatch(
				genericResult, overrideConfig.FieldPath, overrideConfig.FieldFormat, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.FieldMergePatchType:
			genericResult, err = applyFieldMergePatch(
				genericResult, overrideConfig.FieldPath, overrideConfig.FieldFormat, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		default:
			return nil, nil, fmt.Errorf("unsupported OverrideType %s", overrideConfig.Type)
		}
	}

	return genericResult, chartResult, nil
}

// indent is used to format the JSON patch result
// for JSONPatchType, the indent is "" by default
// for FieldPatchType, the indent is "    " for pretty printing
func applyJSONPatch(cur, overrideBytes []byte, indent string) ([]byte, error) {
	if strings.TrimSpace(string(cur)) == "" {
		return cur, nil
	}
	patchObj, err := jsonpatch.DecodePatch(overrideBytes)
	if err != nil {
		return nil, err
	}
	if len(patchObj) > maxJSONPatchOperations {
		return nil, fmt.Errorf("the allowed maximum operations in a JSON patch is %d, got %d",
			maxJSONPatchOperations, len(patchObj))
	}
	patchedJS, err := patchObj.ApplyIndent(cur, indent)
	if err == nil {
		return patchedJS, nil
	}
	if errors.Is(err, jsonpatch.ErrMissing) {
		return cur, nil
	}
	return nil, err
}

func applyHelmValuesOverride(currentByte, overrideByte []byte) ([]byte, error) {
	currentObj := map[string]interface{}{}
	if err := utils.Unmarshal(currentByte, &currentObj); err != nil {
		return nil, err
	}

	var overrideValues map[string]interface{}
	if err := utils.Unmarshal(overrideByte, &overrideValues); err != nil {
		return nil, err
	}
	return utils.Marshal(chartutil.CoalesceTables(overrideValues, currentObj))
}

func applyFieldJSONPatch(cur []byte, fieldPath string, fieldFormat appsapi.FieldFormatType, overrideBytes []byte) (
	[]byte, error) {

	fieldValue, err := findField(cur, fieldPath)
	if err != nil {
		return nil, err
	}
	if len(fieldValue) == 0 {
		return cur, nil
	}

	var result []byte
	switch fieldFormat {
	case appsapi.JSONFormat:
		result, err = applyJSONPatch([]byte(fieldValue), overrideBytes, jsonIndent)
		if err != nil {
			return nil, err
		}
	case appsapi.YAMLFormat:
		var fieldValueJSON []byte
		var resultJSON []byte
		fieldValueJSON, err = yaml.YAMLToJSON([]byte(fieldValue))
		if err != nil {
			return nil, fmt.Errorf("failed to convert patch %s to JSON: %v", cur, err)
		}
		resultJSON, err = applyJSONPatch(fieldValueJSON, overrideBytes, jsonIndent)
		if err != nil {
			return nil, err
		}
		result, err = yaml.JSONToYAML(resultJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to convert json %s to yaml, error: %v", string(resultJSON), err)
		}
	default:
		return nil, fmt.Errorf("unsupported FieldFormatType %s", fieldFormat)
	}
	patches := []utils.JsonPatchOption{
		{
			Op:    "replace",
			Path:  fieldPath,
			Value: string(result),
		},
	}
	patchesBytes, _ := jsoniter.MarshalToString(patches)
	return applyJSONPatch(
		cur, []byte(patchesBytes), "")
}

func applyFieldMergePatch(cur []byte, fieldPath string, fieldFormat appsapi.FieldFormatType, overrideBytes []byte) (
	[]byte, error) {

	fieldValue, err := findField(cur, fieldPath)
	if err != nil {
		return nil, err
	}
	if len(fieldValue) == 0 {
		return cur, nil
	}

	var result []byte
	switch fieldFormat {
	case appsapi.JSONFormat:
		result, err = jsonpatch.MergePatch([]byte(fieldValue), overrideBytes)
		if err != nil {
			return nil, err
		}
		var tmpObj map[string]interface{}
		if err = json.Unmarshal(result, &tmpObj); err != nil {
			return nil, fmt.Errorf("failed to unmarshal %s to json, error: %v", string(result), err)
		}
		result, err = json.MarshalIndent(tmpObj, nonIndent, jsonIndent)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal %v to json, error: %v", tmpObj, err)
		}
	case appsapi.YAMLFormat:
		var fieldValueJSON []byte
		var resultJSON []byte
		fieldValueJSON, err = yaml.YAMLToJSON([]byte(fieldValue))
		if err != nil {
			return nil, fmt.Errorf("failed to convert patch %s to JSON: %v", cur, err)
		}
		resultJSON, err = jsonpatch.MergePatch(fieldValueJSON, overrideBytes)
		if err != nil {
			return nil, err
		}
		result, err = yaml.JSONToYAML(resultJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to convert json %s to yaml, error: %v", string(resultJSON), err)
		}
	default:
		return nil, fmt.Errorf("unsupported FieldFormatType %s", fieldFormat)
	}
	patches := []utils.JsonPatchOption{
		{
			Op:    "replace",
			Path:  fieldPath,
			Value: string(result),
		},
	}
	patchesBytes, _ := jsoniter.MarshalToString(patches)
	return applyJSONPatch(
		cur, []byte(patchesBytes), "")
}

// findField finds a field in a json
// fieldPath format: /path/to/parts/0/field
func findField(cur []byte, fieldPath string) (string, error) {
	obj := map[string]interface{}{}
	if err := jsoniter.Unmarshal(cur, &obj); err != nil {
		return "", fmt.Errorf("failed to unmarshal %s to json, error: %v", cur, err)
	}
	any := jsoniter.Wrap(obj)

	split := strings.Split(fieldPath, "/")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid fieldPath %s, should start with /", fieldPath)
	}
	parts := split[1:]
	for _, part := range parts {
		var curPath interface{}
		switch any.ValueType() {
		case jsoniter.InvalidValue, jsoniter.NilValue, jsoniter.NumberValue, jsoniter.BoolValue:
			return "", fmt.Errorf("object is not a string or array, cannot index by field %s", part)
		case jsoniter.ArrayValue:
			num, err := strconv.Atoi(part)
			if err != nil {
				return "", fmt.Errorf("object is a array, but convert field %s to number, error: %v", part, err)
			}
			curPath = num
		case jsoniter.ObjectValue:
			curPath = part
		default:
			return "", fmt.Errorf("invalid valueType %v, cannot find field %s", any.ValueType(), part)
		}
		any = any.Get(curPath)
	}
	if any.ValueType() != jsoniter.StringValue {
		return "", fmt.Errorf("field value index by path %s is not a string, but %v", fieldPath, any.ValueType())
	}
	return any.ToString(), nil
}
