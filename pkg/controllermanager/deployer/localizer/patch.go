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
	"errors"
	"fmt"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	"helm.sh/helm/v3/pkg/chartutil"
	"sigs.k8s.io/yaml"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/utils"
)

const (
	// maximum number of operations a single json patch may contain.
	maxJSONPatchOperations = 10000
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
			genericResult, err = applyJSONPatch(genericResult, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		case appsapi.MergePatchType:
			genericResult, err = jsonpatch.MergePatch(genericResult, overrideBytes)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to apply OverrideConfig %s: %v", overrideConfig.Name, err)
			}
		default:
			return nil, nil, fmt.Errorf("unsupported OverrideType %s", overrideConfig.Type)
		}
	}

	return genericResult, chartResult, nil
}

func applyJSONPatch(cur, overrideBytes []byte) ([]byte, error) {
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
	patchedJS, err := patchObj.Apply(cur)
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
