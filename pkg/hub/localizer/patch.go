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
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
)

const (
	// maximum number of operations a single json patch may contain.
	maxJSONPatchOperations = 10000
)

func applyOverrides(original []byte, overrides []appsapi.OverrideConfig) ([]byte, error) {
	result := original
	for _, overrideConfig := range overrides {
		overrideBytes, err := yaml.YAMLToJSON([]byte(overrideConfig.Value))
		if err != nil {
			return nil, fmt.Errorf("failed to convert patch %s to JSON: %v", overrideConfig.Value, err)
		}

		// validation before apply override
		if len(strings.TrimSpace(string(result))) == 0 {
			result = overrideBytes
			continue
		}

		if len(strings.TrimSpace(string(overrideBytes))) == 0 {
			continue
		}

		switch overrideConfig.Type {
		case appsapi.HelmType:
			result, err = applyHelmOverride(result, overrideBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.JSONPatchType:
			result, err = applyJSONPatch(result, overrideBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.MergePatchType:
			result, err = jsonpatch.MergePatch(result, overrideBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		default:
			return nil, fmt.Errorf("unsupported OverrideType %s", overrideConfig.Type)
		}
	}

	return result, nil
}

func applyJSONPatch(cur, overrideBytes []byte) ([]byte, error) {
	patchObj, err := jsonpatch.DecodePatch(overrideBytes)
	if err != nil {
		return nil, err
	}
	if len(patchObj) > maxJSONPatchOperations {
		return nil, fmt.Errorf("the allowed maximum operations in a JSON patch is %d, got %d",
			maxJSONPatchOperations, len(patchObj))
	}
	patchedJS, err := patchObj.Apply(cur)
	if err != nil {
		return nil, err
	}
	return patchedJS, nil
}

func applyHelmOverride(currentByte, overrideByte []byte) ([]byte, error) {
	currentObj := map[string]interface{}{}
	if err := json.Unmarshal(currentByte, &currentObj); err != nil {
		return nil, err
	}

	var overrideValues map[string]interface{}
	if err := json.Unmarshal(overrideByte, &overrideValues); err != nil {
		return nil, err
	}
	return json.Marshal(chartutil.CoalesceTables(overrideValues, currentObj))
}
