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

// NewAnyNotInHandler returns handler to manage AnyNotIn operations
func NewAnyNotInHandler(ctx context.EvalInterface) OperatorHandler {
	return AnyNotInHandler{
		ctx: ctx,
	}
}

// AnyNotInHandler provides implementation to handle AnyNotIn Operator
type AnyNotInHandler struct {
	ctx context.EvalInterface
}

// Evaluate evaluates expression with AnyNotIn Operator
func (anynin AnyNotInHandler) Evaluate(key, value interface{}) bool {
	switch typedKey := key.(type) {
	case string:
		return anynin.validateValueWithStringPattern(typedKey, value)
	case int, int32, int64, float32, float64:
		return anynin.validateValueWithStringPattern(fmt.Sprint(typedKey), value)
	case []interface{}:
		var stringSlice []string
		for _, v := range typedKey {
			stringSlice = append(stringSlice, fmt.Sprint(v))
		}
		return anynin.validateValueWithStringSetPattern(stringSlice, value)
	default:
		klog.V(2).Info("Unsupported type", "value", typedKey, "type", fmt.Sprintf("%T", typedKey))
		return false
	}
}

func (anynin AnyNotInHandler) validateValueWithStringPattern(key string, value interface{}) bool {
	invalidType, keyExists := anyKeyExistsInArray(key, value)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return !keyExists
}

func (anynin AnyNotInHandler) validateValueWithStringSetPattern(key []string, value interface{}) bool {
	invalidType, isAnyNotIn := anySetExistsInArray(key, value, true)
	if invalidType {
		klog.V(2).Info("expected type []string", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}

	return isAnyNotIn
}

func (anynin AnyNotInHandler) validateValueWithBoolPattern(_ bool, _ interface{}) bool {
	return false
}

func (anynin AnyNotInHandler) validateValueWithIntPattern(_ int64, _ interface{}) bool {
	return false
}

func (anynin AnyNotInHandler) validateValueWithFloatPattern(_ float64, _ interface{}) bool {
	return false
}

func (anynin AnyNotInHandler) validateValueWithMapPattern(_ map[string]interface{}, _ interface{}) bool {
	return false
}

func (anynin AnyNotInHandler) validateValueWithSlicePattern(_ []interface{}, _ interface{}) bool {
	return false
}
