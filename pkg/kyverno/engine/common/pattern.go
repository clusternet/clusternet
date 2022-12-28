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

package common

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/operator"
	wildcard "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/wildcard"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

type quantity int

const (
	equal       quantity = 0
	lessThan    quantity = -1
	greaterThan quantity = 1
)

// ValidateValueWithPattern validates value with operators and wildcards
func ValidateValueWithPattern(value, pattern interface{}) bool {
	switch typedPattern := pattern.(type) {
	case bool:
		typedValue, ok := value.(bool)
		if !ok {
			klog.V(4).Info("Expected type bool", "type", fmt.Sprintf("%T", value), "value", value)
			return false
		}
		return typedPattern == typedValue
	case int:
		return validateValueWithIntPattern(value, int64(typedPattern))
	case int64:
		return validateValueWithIntPattern(value, typedPattern)
	case float64:
		return validateValueWithFloatPattern(value, typedPattern)
	case string:
		return validateValueWithStringPatterns(value, typedPattern)
	case nil:
		return validateValueWithNilPattern(value)
	case map[string]interface{}:
		return validateValueWithMapPattern(value, typedPattern)
	case []interface{}:
		klog.V(2).Info("arrays are not supported as patterns")
		return false
	default:
		klog.V(2).Info("Unknown type", "type", fmt.Sprintf("%T", typedPattern), "value", typedPattern)
		return false
	}
}

func validateValueWithMapPattern(value interface{}, typedPattern map[string]interface{}) bool {
	// verify the type of the resource value is map[string]interface,
	// we only check for existence of object, not the equality of content and value
	_, ok := value.(map[string]interface{})
	if !ok {
		klog.V(2).Info("Expected type map[string]interface{}", "type", fmt.Sprintf("%T", value), "value", value)
		return false
	}
	return true
}

// Handler for int values during validation process
func validateValueWithIntPattern(value interface{}, pattern int64) bool {
	switch typedValue := value.(type) {
	case int:
		return int64(typedValue) == pattern
	case int64:
		return typedValue == pattern
	case float64:
		// check that float has no fraction
		if typedValue == math.Trunc(typedValue) {
			return int64(typedValue) == pattern
		}

		klog.V(2).Info("Expected type int", "type", fmt.Sprintf("%T", typedValue), "value", typedValue)
		return false
	case string:
		// extract int64 from string
		int64Num, err := strconv.ParseInt(typedValue, 10, 64)
		if err != nil {
			klog.Error(err, "Failed to parse int64 from string")
			return false
		}
		return int64Num == pattern
	default:
		klog.V(2).Info("Expected type int", "type", fmt.Sprintf("%T", value), "value", value)
		return false
	}
}

// Handler for float values during validation process
func validateValueWithFloatPattern(value interface{}, pattern float64) bool {
	switch typedValue := value.(type) {
	case int:
		// check that float has no fraction
		if pattern == math.Trunc(pattern) {
			return int(pattern) == value
		}
		klog.V(2).Info("Expected type float", "type", fmt.Sprintf("%T", typedValue), "value", typedValue)
		return false
	case int64:
		// check that float has no fraction
		if pattern == math.Trunc(pattern) {
			return int64(pattern) == value
		}
		klog.V(2).Info("Expected type float", "type", fmt.Sprintf("%T", typedValue), "value", typedValue)
		return false
	case float64:
		return typedValue == pattern
	case string:
		// extract float64 from string
		float64Num, err := strconv.ParseFloat(typedValue, 64)
		if err != nil {
			klog.Error(err, "Failed to parse float64 from string")
			return false
		}
		return float64Num == pattern
	default:
		klog.V(2).Info("Expected type float", "type", fmt.Sprintf("%T", value), "value", value)
		return false
	}
}

// Handler for nil values during validation process
func validateValueWithNilPattern(value interface{}) bool {
	switch typed := value.(type) {
	case float64:
		return typed == 0.0
	case int:
		return typed == 0
	case int64:
		return typed == 0
	case string:
		return typed == ""
	case bool:
		return !typed
	case nil:
		return true
	case map[string]interface{}, []interface{}:
		klog.V(2).Info("Maps and arrays could not be checked with nil pattern")
		return false
	default:
		klog.V(2).Info("Unknown type as value when checking for nil pattern", "type", fmt.Sprintf("%T", value), "value", value)
		return false
	}
}

// Handler for pattern values during validation process
func validateValueWithStringPatterns(value interface{}, pattern string) bool {
	if value == pattern {
		return true
	}

	conditions := strings.Split(pattern, "|")
	for _, condition := range conditions {
		condition = strings.Trim(condition, " ")
		if checkForAndConditionsAndValidate(value, condition) {
			return true
		}
	}

	return false
}

func checkForAndConditionsAndValidate(value interface{}, pattern string) bool {
	conditions := strings.Split(pattern, "&")
	for _, condition := range conditions {
		condition = strings.Trim(condition, " ")
		if !validateValueWithStringPattern(value, condition) {
			return false
		}
	}

	return true
}

// Handler for single pattern value during validation process
// Detects if pattern has a number
func validateValueWithStringPattern(value interface{}, pattern string) bool {
	operatorVariable := operator.GetOperatorFromStringPattern(pattern)

	// Upon encountering InRange operator split the string by `-` and basically
	// verify the result of (x >= leftEndpoint & x <= rightEndpoint)
	if operatorVariable == operator.InRange {
		endpoints := strings.Split(pattern, "-")
		leftEndpoint, rightEndpoint := endpoints[0], endpoints[1]

		gt := validateValueWithStringPattern(value, fmt.Sprintf(">=%s", leftEndpoint))
		if !gt {
			return false
		}
		pattern = fmt.Sprintf("<=%s", rightEndpoint)
		operatorVariable = operator.LessEqual
	}

	// Upon encountering NotInRange operator split the string by `!-` and basically
	// verify the result of (x < leftEndpoint | x > rightEndpoint)
	if operatorVariable == operator.NotInRange {
		endpoints := strings.Split(pattern, "!-")
		leftEndpoint, rightEndpoint := endpoints[0], endpoints[1]

		lt := validateValueWithStringPattern(value, fmt.Sprintf("<%s", leftEndpoint))
		if lt {
			return true
		}
		pattern = fmt.Sprintf(">%s", rightEndpoint)
		operatorVariable = operator.More
	}

	pattern = pattern[len(operatorVariable):]
	pattern = strings.TrimSpace(pattern)
	number, str := getNumberAndStringPartsFromPattern(pattern)

	if number == "" {
		return validateString(value, str, operatorVariable)
	}

	return validateNumberWithStr(value, pattern, operatorVariable)
}

// Handler for string values
func validateString(value interface{}, pattern string, operatorVariable operator.Operator) bool {
	if operator.NotEqual == operatorVariable || operator.Equal == operatorVariable {
		var strValue string
		var ok bool = false
		switch v := value.(type) {
		case float64:
			strValue = strconv.FormatFloat(v, 'E', -1, 64)
			ok = true
		case int:
			strValue = strconv.FormatInt(int64(v), 10)
			ok = true
		case int64:
			strValue = strconv.FormatInt(v, 10)
			ok = true
		case string:
			strValue = v
			ok = true
		case bool:
			strValue = strconv.FormatBool(v)
			ok = true
		case nil:
			ok = false
		}
		if !ok {
			klog.V(4).Info("unexpected type", "got", value, "expect", pattern)
			return false
		}

		wildcardResult := wildcard.Match(pattern, strValue)

		if operator.NotEqual == operatorVariable {
			return !wildcardResult
		}

		return wildcardResult
	}
	klog.V(2).Info("Operators >, >=, <, <= are not applicable to strings")
	return false
}

// validateNumberWithStr compares quantity if pattern type is quantity
// or a wildcard match to pattern string
func validateNumberWithStr(value interface{}, pattern string, operator operator.Operator) bool {
	typedValue, err := convertNumberToString(value)
	if err != nil {
		klog.Error(err, "failed to convert to string")
		return false
	}

	patternQuan, err := apiresource.ParseQuantity(pattern)
	// 1. nil error - quantity comparison
	if err == nil {
		valueQuan, err := apiresource.ParseQuantity(typedValue)
		if err != nil {
			klog.Error(err, "invalid quantity in resource", "type", fmt.Sprintf("%T", typedValue), "value", typedValue)
			return false
		}

		return compareQuantity(valueQuan, patternQuan, operator)
	}

	// 2. wildcard match
	if validateString(value, pattern, operator) {
		return true
	} else {
		klog.V(4).Info("value failed wildcard check", "type", fmt.Sprintf("%T", typedValue), "value", typedValue, "check", pattern)
		return false
	}
}

func compareQuantity(value, pattern apiresource.Quantity, op operator.Operator) bool {
	result := value.Cmp(pattern)
	switch op {
	case operator.Equal:
		return result == int(equal)
	case operator.NotEqual:
		return result != int(equal)
	case operator.More:
		return result == int(greaterThan)
	case operator.Less:
		return result == int(lessThan)
	case operator.MoreEqual:
		return (result == int(equal)) || (result == int(greaterThan))
	case operator.LessEqual:
		return (result == int(equal)) || (result == int(lessThan))
	}

	return false
}

// detects numerical and string parts in pattern and returns them
func getNumberAndStringPartsFromPattern(pattern string) (number, str string) {
	regexpStr := `^(\d*(\.\d+)?)(.*)`
	re := regexp.MustCompile(regexpStr)
	matches := re.FindAllStringSubmatch(pattern, -1)
	match := matches[0]
	return match[1], match[3]
}
