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
	"time"

	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"k8s.io/klog/v2"
)

// NewDurationOperatorHandler returns handler to manage the provided duration operations (>, >=, <=, <)
func NewDurationOperatorHandler(ctx context.EvalInterface, op kyvernov1.ConditionOperator) OperatorHandler {
	return DurationOperatorHandler{
		ctx:       ctx,
		condition: op,
	}
}

// DurationOperatorHandler provides implementation to handle Duration Operations associated with policies
type DurationOperatorHandler struct {
	ctx       context.EvalInterface
	condition kyvernov1.ConditionOperator
}

// durationCompareByCondition compares a time.Duration key with a time.Duration value on the basis of the provided operator
func durationCompareByCondition(key time.Duration, value time.Duration, op kyvernov1.ConditionOperator) bool {
	switch op {
	case kyvernov1.ConditionOperators["DurationGreaterThanOrEquals"]:
		return key >= value
	case kyvernov1.ConditionOperators["DurationGreaterThan"]:
		return key > value
	case kyvernov1.ConditionOperators["DurationLessThanOrEquals"]:
		return key <= value
	case kyvernov1.ConditionOperators["DurationLessThan"]:
		return key < value
	default:
		klog.V(2).Info(fmt.Sprintf("Expected operator, one of [DurationGreaterThanOrEquals, DurationGreaterThan, DurationLessThanOrEquals, DurationLessThan], found %s", op))
		return false
	}
}

func (doh DurationOperatorHandler) Evaluate(key, value interface{}) bool {
	switch typedKey := key.(type) {
	case int:
		return doh.validateValueWithIntPattern(int64(typedKey), value)
	case int64:
		return doh.validateValueWithIntPattern(typedKey, value)
	case float64:
		return doh.validateValueWithFloatPattern(typedKey, value)
	case string:
		return doh.validateValueWithStringPattern(typedKey, value)
	default:
		klog.V(2).Info("Unsupported type", "value", typedKey, "type", fmt.Sprintf("%T", typedKey))
		return false
	}
}

func (doh DurationOperatorHandler) validateValueWithIntPattern(key int64, value interface{}) bool {
	switch typedValue := value.(type) {
	case int:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case int64:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case float64:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case string:
		duration, err := time.ParseDuration(typedValue)
		if err == nil {
			return durationCompareByCondition(time.Duration(key)*time.Second, duration, doh.condition)
		}
		klog.Error(fmt.Errorf("parse error: "), "Failed to parse time duration from the string value")
		return false
	default:
		klog.V(2).Info("Unexpected type", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}
}

func (doh DurationOperatorHandler) validateValueWithFloatPattern(key float64, value interface{}) bool {
	switch typedValue := value.(type) {
	case int:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case int64:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case float64:
		return durationCompareByCondition(time.Duration(key)*time.Second, time.Duration(typedValue)*time.Second, doh.condition)
	case string:
		duration, err := time.ParseDuration(typedValue)
		if err == nil {
			return durationCompareByCondition(time.Duration(key)*time.Second, duration, doh.condition)
		}
		klog.Error(fmt.Errorf("parse error: "), "Failed to parse time duration from the string value")
		return false
	default:
		klog.V(2).Info("Unexpected type", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}
}

func (doh DurationOperatorHandler) validateValueWithStringPattern(key string, value interface{}) bool {
	duration, err := time.ParseDuration(key)
	if err != nil {
		klog.Error(err, "Failed to parse time duration from the string key")
		return false
	}
	switch typedValue := value.(type) {
	case int:
		return durationCompareByCondition(duration, time.Duration(typedValue)*time.Second, doh.condition)
	case int64:
		return durationCompareByCondition(duration, time.Duration(typedValue)*time.Second, doh.condition)
	case float64:
		return durationCompareByCondition(duration, time.Duration(typedValue)*time.Second, doh.condition)
	case string:
		durationValue, err := time.ParseDuration(typedValue)
		if err == nil {
			return durationCompareByCondition(duration, durationValue, doh.condition)
		}
		klog.Error(fmt.Errorf("parse error: "), "Failed to parse time duration from the string value")
		return false
	default:
		klog.V(2).Info("Unexpected type", "value", value, "type", fmt.Sprintf("%T", value))
		return false
	}
}

// the following functions are unreachable because the key is strictly supposed to be a duration
// still the following functions are just created to make DurationOperatorHandler struct implement OperatorHandler interface
func (doh DurationOperatorHandler) validateValueWithBoolPattern(key bool, value interface{}) bool {
	return false
}

func (doh DurationOperatorHandler) validateValueWithMapPattern(key map[string]interface{}, value interface{}) bool {
	return false
}

func (doh DurationOperatorHandler) validateValueWithSlicePattern(key []interface{}, value interface{}) bool {
	return false
}
