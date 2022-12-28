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
	"fmt"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"k8s.io/klog/v2"
)

// NewNotInHandler returns handler to manage NotIn operations
//
// Deprecated: Use `NewAllNotInHandler` or `NewAnyNotInHandler` instead
func NewNotInHandler(ctx context.EvalInterface) OperatorHandler {
	return NotInHandler{
		ctx: ctx,
	}
}

// NotInHandler provides implementation to handle NotIn Operator
type NotInHandler struct {
	ctx context.EvalInterface
}

// Evaluate evaluates expression with NotIn Operator
func (nin NotInHandler) Evaluate(key, value interface{}) bool {
	switch typedKey := key.(type) {
	case string:
		return nin.validateValueWithStringPattern(typedKey, value)
	case int, int32, int64, float32, float64:
		return nin.validateValueWithStringPattern(fmt.Sprint(typedKey), value)
	case []interface{}:
		var stringSlice []string
		for _, v := range typedKey {
			stringSlice = append(stringSlice, v.(string))
		}
		return nin.validateValueWithStringSetPattern(stringSlice, value)
	default:
		klog.V(2).Info("Unsupported type", "value", typedKey, "type", fmt.Sprintf("%T", typedKey))
		return false
	}
}

func (nin NotInHandler) validateValueWithStringPattern(key string, value interface{}) bool {
	invalidType, keyExists := keyExistsInArray(key, value)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return !keyExists
}

func (nin NotInHandler) validateValueWithStringSetPattern(key []string, value interface{}) bool {
	invalidType, isNotIn := setExistsInArray(key, value, true)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return isNotIn
}

func (nin NotInHandler) validateValueWithBoolPattern(_ bool, _ interface{}) bool {
	return false
}

func (nin NotInHandler) validateValueWithIntPattern(_ int64, _ interface{}) bool {
	return false
}

func (nin NotInHandler) validateValueWithFloatPattern(_ float64, _ interface{}) bool {
	return false
}

func (nin NotInHandler) validateValueWithMapPattern(_ map[string]interface{}, _ interface{}) bool {
	return false
}

func (nin NotInHandler) validateValueWithSlicePattern(_ []interface{}, _ interface{}) bool {
	return false
}
