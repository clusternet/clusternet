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

package operator

import (
	"encoding/json"
	"fmt"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	wildcard "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/wildcard"
	"k8s.io/klog/v2"
)

// NewInHandler returns handler to manage In operations
//
// Deprecated: Use `NewAllInHandler` or `NewAnyInHandler` instead
func NewInHandler(ctx context.EvalInterface) OperatorHandler {
	return InHandler{
		ctx: ctx,
	}
}

// InHandler provides implementation to handle In Operator
type InHandler struct {
	ctx context.EvalInterface
}

// Evaluate evaluates expression with In Operator
func (in InHandler) Evaluate(key, value interface{}) bool {
	switch typedKey := key.(type) {
	case string:
		return in.validateValueWithStringPattern(typedKey, value)
	case int, int32, int64, float32, float64:
		return in.validateValueWithStringPattern(fmt.Sprint(typedKey), value)
	case []interface{}:
		var stringSlice []string
		for _, v := range typedKey {
			stringSlice = append(stringSlice, v.(string))
		}
		return in.validateValueWithStringSetPattern(stringSlice, value)
	default:
		klog.V(2).Info("Unsupported type", "value", typedKey, "type", fmt.Sprintf("%T", typedKey))
		return false
	}
}

func (in InHandler) validateValueWithStringPattern(key string, value interface{}) (keyExists bool) {
	invalidType, keyExists := keyExistsInArray(key, value)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return keyExists
}

// keyExistsInArray checks if the  key exists in the array value
// The value can be a string, an array of strings, or a JSON format
// array of strings (e.g. ["val1", "val2", "val3"].
func keyExistsInArray(key string, value interface{}) (invalidType bool, keyExists bool) {
	switch valuesAvailable := value.(type) {
	case []interface{}:
		for _, val := range valuesAvailable {
			if wildcard.Match(fmt.Sprint(val), key) || wildcard.Match(key, fmt.Sprint(val)) {
				return false, true
			}
		}

	case string:
		if wildcard.Match(valuesAvailable, key) {
			return false, true
		}

		var arr []string
		if err := json.Unmarshal([]byte(valuesAvailable), &arr); err != nil {
			klog.Error(err, "failed to unmarshal value to JSON string array", "key", key, "value", value)
			return true, false
		}

		for _, val := range arr {
			if key == val {
				return false, true
			}
		}

	default:
		invalidType = true
		return
	}

	return false, false
}

func (in InHandler) validateValueWithStringSetPattern(key []string, value interface{}) (keyExists bool) {
	invalidType, isIn := setExistsInArray(key, value, false)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return isIn
}

// setExistsInArray checks if the key is a subset of value
// The value can be a string, an array of strings, or a JSON format
// array of strings (e.g. ["val1", "val2", "val3"].
// notIn argument if set to true will check for NotIn
func setExistsInArray(key []string, value interface{}, notIn bool) (invalidType bool, keyExists bool) {
	switch valuesAvailable := value.(type) {
	case []interface{}:
		var valueSlice []string
		for _, val := range valuesAvailable {
			v, ok := val.(string)
			if !ok {
				return true, false
			}
			valueSlice = append(valueSlice, v)
		}
		if notIn {
			return false, isNotIn(key, valueSlice)
		}
		return false, isIn(key, valueSlice)

	case string:

		if len(key) == 1 && key[0] == valuesAvailable {
			return false, true
		}

		var arr []string
		if err := json.Unmarshal([]byte(valuesAvailable), &arr); err != nil {
			klog.Error(err, "failed to unmarshal value to JSON string array", "key", key, "value", value)
			return true, false
		}
		if notIn {
			return false, isNotIn(key, arr)
		}

		return false, isIn(key, arr)

	default:
		return true, false
	}
}

// isIn checks if all values in S1 are in S2
func isIn(key []string, value []string) bool {
	set := make(map[string]bool)

	for _, val := range value {
		set[val] = true
	}

	for _, val := range key {
		_, found := set[val]
		if !found {
			return false
		}
	}

	return true
}

// isNotIn checks if any of the values in S1 is not in S2
func isNotIn(key []string, value []string) bool {
	set := make(map[string]bool)

	for _, val := range value {
		set[val] = true
	}

	for _, val := range key {
		_, found := set[val]
		if !found {
			return true
		}
	}

	return false
}

func (in InHandler) validateValueWithBoolPattern(_ bool, _ interface{}) bool {
	return false
}

func (in InHandler) validateValueWithIntPattern(_ int64, _ interface{}) bool {
	return false
}

func (in InHandler) validateValueWithFloatPattern(_ float64, _ interface{}) bool {
	return false
}

func (in InHandler) validateValueWithMapPattern(_ map[string]interface{}, _ interface{}) bool {
	return false
}

func (in InHandler) validateValueWithSlicePattern(_ []interface{}, _ interface{}) bool {
	return false
}
