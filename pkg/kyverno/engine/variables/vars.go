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

package variables

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/anchor"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	jsonUtils "github.com/clusternet/clusternet/pkg/kyverno/engine/jsonutils"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/operator"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/utils/jsonpointer"
	gojmespath "github.com/jmespath/go-jmespath"
	"k8s.io/klog/v2"
)

var RegexVariables = regexp.MustCompile(`(?:^|[^\\])(\{\{(?:\{[^{}]*\}|[^{}])*\}\})`)

var RegexEscpVariables = regexp.MustCompile(`\\\{\{(\{[^{}]*\}|[^{}])*\}\}`)

// RegexReferences is the Regex for '$(...)' at the beginning of the string, and 'x$(...)' where 'x' is not '\'
var RegexReferences = regexp.MustCompile(`^\$\(.[^\ ]*\)|[^\\]\$\(.[^\ ]*\)`)

// RegexEscpReferences is the Regex for '\$(...)'
var RegexEscpReferences = regexp.MustCompile(`\\\$\(.[^\ ]*\)`)

var regexVariableInit = regexp.MustCompile(`^\{\{(\{[^{}]*\}|[^{}])*\}\}`)

// IsVariable returns true if the element contains a 'valid' variable {{}}
func IsVariable(value string) bool {
	groups := RegexVariables.FindAllStringSubmatch(value, -1)
	return len(groups) != 0
}

// IsReference returns true if the element contains a 'valid' reference $()
func IsReference(value string) bool {
	groups := RegexReferences.FindAllStringSubmatch(value, -1)
	return len(groups) != 0
}

// ReplaceAllVars replaces all variables with the value defined in the replacement function
// This is used to avoid validation errors
func ReplaceAllVars(src string, repl func(string) string) string {
	wrapper := func(s string) string {
		initial := len(regexVariableInit.FindAllString(s, -1)) > 0
		prefix := ""

		if !initial {
			prefix = string(s[0])
			s = s[1:]
		}

		return prefix + repl(s)
	}

	return RegexVariables.ReplaceAllStringFunc(src, wrapper)
}

func newPreconditionsVariableResolver() VariableResolver {
	// PreconditionsVariableResolver is used to substitute vars in preconditions.
	// It returns an empty string if an error occurs during the substitution.
	return func(ctx context.EvalInterface, variable string) (interface{}, error) {
		value, err := DefaultVariableResolver(ctx, variable)
		if err != nil {
			klog.V(4).Info(fmt.Sprintf("Variable substitution failed in preconditions, therefore nil value assigned to variable,  \"%s\" ", variable))
			return value, err
		}

		return value, nil
	}
}

// SubstituteAll substitutes variables and references in the document. The document must be JSON data
// i.e. string, []interface{}, map[string]interface{}
func SubstituteAll(ctx context.EvalInterface, document interface{}) (_ interface{}, err error) {
	return substituteAll(ctx, document, DefaultVariableResolver)
}

func SubstituteAllInPreconditions(ctx context.EvalInterface, document interface{}) (_ interface{}, err error) {
	// We must convert all incoming conditions to JSON data i.e.
	// string, []interface{}, map[string]interface{}
	// we cannot use structs otherwise json traverse doesn't work
	untypedDoc, err := DocumentToUntyped(document)
	if err != nil {
		return document, err
	}
	return substituteAll(ctx, untypedDoc, newPreconditionsVariableResolver())
}

func SubstituteAllInPatchConfig(ctx context.EvalInterface, typedConfig kyvernov1.KyvernoPatchConfig) (
	kyvernov1.KyvernoPatchConfig, error) {
	var err error
	var config interface{}
	config, err = DocumentToUntyped(typedConfig)
	if err != nil {
		return typedConfig, err
	}

	config, err = SubstituteAll(ctx, config)
	if err != nil {
		return typedConfig, err
	}

	return UntypedToPatchConfig(config)
}

func DocumentToUntyped(doc interface{}) (interface{}, error) {
	jsonDoc, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}

	var untyped interface{}
	err = json.Unmarshal(jsonDoc, &untyped)
	if err != nil {
		return nil, err
	}

	return untyped, nil
}

func UntypedToPatchConfig(untyped interface{}) (kyvernov1.KyvernoPatchConfig, error) {
	jsonConfig, err := json.Marshal(untyped)
	if err != nil {
		return kyvernov1.KyvernoPatchConfig{}, err
	}

	var config kyvernov1.KyvernoPatchConfig
	err = json.Unmarshal(jsonConfig, &config)
	if err != nil {
		return kyvernov1.KyvernoPatchConfig{}, err
	}

	return config, nil
}

// func UntypedToRule(untyped interface{}) (kyvernov1.Rule, error) {
// 	jsonRule, err := json.Marshal(untyped)
// 	if err != nil {
// 		return kyvernov1.Rule{}, err
// 	}

// 	var rule kyvernov1.Rule
// 	err = json.Unmarshal(jsonRule, &rule)
// 	if err != nil {
// 		return kyvernov1.Rule{}, err
// 	}

// 	return rule, nil
// }

func SubstituteAllInConditions(ctx context.EvalInterface, conditions []kyvernov1.AnyAllConditions) ([]kyvernov1.AnyAllConditions, error) {
	c, err := ConditionsToJSONObject(conditions)
	if err != nil {
		return nil, err
	}

	i, err := SubstituteAll(ctx, c)
	if err != nil {
		return nil, err
	}

	return JSONObjectToConditions(i)
}

func ConditionsToJSONObject(conditions []kyvernov1.AnyAllConditions) ([]map[string]interface{}, error) {
	bytes, err := json.Marshal(conditions)
	if err != nil {
		return nil, err
	}

	m := []map[string]interface{}{}
	if err := json.Unmarshal(bytes, &m); err != nil {
		return nil, err
	}

	return m, nil
}

func JSONObjectToConditions(data interface{}) ([]kyvernov1.AnyAllConditions, error) {
	bytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var c []kyvernov1.AnyAllConditions
	if err := json.Unmarshal(bytes, &c); err != nil {
		return nil, err
	}

	return c, nil
}

func substituteAll(ctx context.EvalInterface, document interface{}, resolver VariableResolver) (_ interface{}, err error) {
	document, err = substituteReferences(document)
	if err != nil {
		return document, err
	}

	return substituteVars(ctx, document, resolver)
}

func substituteVars(ctx context.EvalInterface, rule interface{}, vr VariableResolver) (interface{}, error) {
	return jsonUtils.NewTraversal(rule, substituteVariablesIfAny(ctx, vr)).TraverseJSON()
}

func substituteReferences(rule interface{}) (interface{}, error) {
	return jsonUtils.NewTraversal(rule, substituteReferencesIfAny()).TraverseJSON()
}

func ValidateElementInForEach(rule interface{}) (interface{}, error) {
	return jsonUtils.NewTraversal(rule, validateElementInForEach()).TraverseJSON()
}

func validateElementInForEach() jsonUtils.Action {
	return jsonUtils.OnlyForLeafsAndKeys(func(data *jsonUtils.ActionData) (interface{}, error) {
		value, ok := data.Element.(string)
		if !ok {
			return data.Element, nil
		}
		vars := RegexVariables.FindAllString(value, -1)
		for _, v := range vars {
			initial := len(regexVariableInit.FindAllString(v, -1)) > 0

			if !initial {
				v = v[1:]
			}

			variable := replaceBracesAndTrimSpaces(v)
			isElementVar := strings.HasPrefix(variable, "element") || variable == "elementIndex"
			if isElementVar && !strings.Contains(data.Path, "/foreach/") {
				return nil, fmt.Errorf("variable '%v' present outside of foreach at path %s", variable, data.Path)
			}
		}
		return nil, nil
	})
}

// NotResolvedReferenceError is returned when it is impossible to resolve the variable
type NotResolvedReferenceError struct {
	reference string
	path      string
}

func (n NotResolvedReferenceError) Error() string {
	return fmt.Sprintf("NotResolvedReferenceErr,reference %s not resolved at path %s", n.reference, n.path)
}

func substituteReferencesIfAny() jsonUtils.Action {
	return jsonUtils.OnlyForLeafsAndKeys(func(data *jsonUtils.ActionData) (interface{}, error) {
		value, ok := data.Element.(string)
		if !ok {
			return data.Element, nil
		}

		for _, v := range RegexReferences.FindAllString(value, -1) {
			initial := v[:2] == `$(`
			old := v

			if !initial {
				v = v[1:]
			}

			resolvedReference, err := resolveReference(data.Document, v, data.Path)
			if err != nil {
				switch err.(type) {
				case context.InvalidVariableError:
					return nil, err
				default:
					return nil, fmt.Errorf("failed to resolve %v at path %s: %v", v, data.Path, err)
				}
			}

			if resolvedReference == nil {
				return data.Element, fmt.Errorf("got nil resolved variable %v at path %s: %v", v, data.Path, err)
			}

			klog.V(3).Info("reference resolved", "reference", v, "value", resolvedReference, "path", data.Path)

			if val, ok := resolvedReference.(string); ok {
				replacement := ""

				if !initial {
					replacement = string(old[0])
				}

				replacement += val

				value = strings.Replace(value, old, replacement, 1)
				continue
			}

			return data.Element, NotResolvedReferenceError{
				reference: v,
				path:      data.Path,
			}
		}

		for _, v := range RegexEscpReferences.FindAllString(value, -1) {
			value = strings.Replace(value, v, v[1:], -1)
		}

		return value, nil
	})
}

// VariableResolver defines the handler function for variable substitution
type VariableResolver = func(ctx context.EvalInterface, variable string) (interface{}, error)

// DefaultVariableResolver is used in all variable substitutions except preconditions
func DefaultVariableResolver(ctx context.EvalInterface, variable string) (interface{}, error) {
	return ctx.Query(variable)
}

func substituteVariablesIfAny(ctx context.EvalInterface, vr VariableResolver) jsonUtils.Action {
	return jsonUtils.OnlyForLeafsAndKeys(func(data *jsonUtils.ActionData) (interface{}, error) {
		value, ok := data.Element.(string)
		if !ok {
			return data.Element, nil
		}

		isDeleteRequest := IsDeleteRequest(ctx)

		vars := RegexVariables.FindAllString(value, -1)
		for len(vars) > 0 {
			originalPattern := value
			for _, v := range vars {
				initial := len(regexVariableInit.FindAllString(v, -1)) > 0
				old := v

				if !initial {
					v = v[1:]
				}

				variable := replaceBracesAndTrimSpaces(v)

				if variable == "@" {
					pathPrefix := "target"
					if _, err := ctx.Query("target"); err != nil {
						pathPrefix = "request.object"
					}

					// Convert path to JMESPath for current identifier.
					// Skip 2 elements (e.g. mutate.overlay | validate.pattern) plus "foreach" if it is part of the pointer.
					// Prefix the pointer with pathPrefix.
					val := jsonpointer.ParsePath(data.Path).SkipPast("foreach").SkipN(2).Prepend(strings.Split(pathPrefix, ".")...).JMESPath()

					variable = strings.Replace(variable, "@", val, -1)
				}

				if isDeleteRequest {
					variable = strings.ReplaceAll(variable, "request.object", "request.oldObject")
				}

				substitutedVar, err := vr(ctx, variable)
				if err != nil {
					switch err.(type) {
					case context.InvalidVariableError, gojmespath.NotFoundError:
						return nil, err
					default:
						return nil, fmt.Errorf("failed to resolve %v at path %s: %v", variable, data.Path, err)
					}
				}

				klog.V(3).Info("variable substituted", "variable", v, "value", substitutedVar, "path", data.Path)

				if originalPattern == v {
					return substitutedVar, nil
				}

				prefix := ""

				if !initial {
					prefix = string(old[0])
				}

				if value, err = substituteVarInPattern(prefix, originalPattern, v, substitutedVar); err != nil {
					return nil, fmt.Errorf("failed to resolve %v at path %s: %s", variable, data.Path, err.Error())
				}

				continue
			}

			// check for nested variables in strings
			vars = RegexVariables.FindAllString(value, -1)
		}

		for _, v := range RegexEscpVariables.FindAllString(value, -1) {
			value = strings.Replace(value, v, v[1:], -1)
		}

		return value, nil
	})
}

func IsDeleteRequest(ctx context.EvalInterface) bool {
	if ctx == nil {
		return false
	}

	operation, err := ctx.Query("request.operation")
	if err == nil && operation == "DELETE" {
		return true
	}

	return false
}

func substituteVarInPattern(prefix, pattern, variable string, value interface{}) (string, error) {
	var stringToSubstitute string

	if s, ok := value.(string); ok {
		stringToSubstitute = s
	} else {
		buffer, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("failed to marshal %T: %v", value, value)
		}
		stringToSubstitute = string(buffer)
	}

	stringToSubstitute = prefix + stringToSubstitute
	variable = prefix + variable

	return strings.Replace(pattern, variable, stringToSubstitute, 1), nil
}

func replaceBracesAndTrimSpaces(v string) string {
	variable := strings.ReplaceAll(v, "{{", "")
	variable = strings.ReplaceAll(variable, "}}", "")
	variable = strings.TrimSpace(variable)
	return variable
}

func resolveReference(fullDocument interface{}, reference, absolutePath string) (interface{}, error) {
	var foundValue interface{}

	path := strings.Trim(reference, "$()")

	operation := operator.GetOperatorFromStringPattern(path)
	path = path[len(operation):]

	if len(path) == 0 {
		return nil, errors.New("expected path, found empty reference")
	}

	path = formAbsolutePath(path, absolutePath)

	valFromReference, err := getValueFromReference(fullDocument, path)
	if err != nil {
		return err, nil
	}

	if operation == operator.Equal { // if operator does not exist return raw value
		return valFromReference, nil
	}

	foundValue, err = valFromReferenceToString(valFromReference, string(operation))
	if err != nil {
		return "", err
	}

	return string(operation) + foundValue.(string), nil
}

// Parse value to string
func valFromReferenceToString(value interface{}, operator string) (string, error) {
	switch typed := value.(type) {
	case string:
		return typed, nil
	case int, int64:
		return fmt.Sprintf("%d", value), nil
	case float64:
		return fmt.Sprintf("%f", value), nil
	default:
		return "", fmt.Errorf("incorrect expression: operator %s does not match with value %v", operator, value)
	}
}

func FindAndShiftReferences(value, shift, pivot string) string {
	for _, reference := range RegexReferences.FindAllString(value, -1) {
		initial := reference[:2] == `$(`
		oldReference := reference

		if !initial {
			reference = reference[1:]
		}

		index := strings.Index(reference, pivot)
		if index == -1 {
			klog.Error(fmt.Errorf(`failed to shift reference: pivot value "%s" was not found`, pivot), "pivot search failed")
		}

		// try to get rule index from the reference
		if pivot == "anyPattern" {
			ruleIndex := strings.Split(reference[index+len(pivot)+1:], "/")[0]
			pivot = pivot + "/" + ruleIndex
		}

		shiftedReference := strings.Replace(reference, pivot, pivot+"/"+shift, -1)
		replacement := ""

		if !initial {
			replacement = string(oldReference[0])
		}

		replacement += shiftedReference

		value = strings.Replace(value, oldReference, replacement, 1)
	}

	return value
}

func formAbsolutePath(referencePath, absolutePath string) string {
	if path.IsAbs(referencePath) {
		return referencePath
	}

	return path.Join(absolutePath, referencePath)
}

func getValueFromReference(fullDocument interface{}, path string) (interface{}, error) {
	var element interface{}

	if _, err := jsonUtils.NewTraversal(fullDocument, jsonUtils.OnlyForLeafsAndKeys(
		func(data *jsonUtils.ActionData) (interface{}, error) {
			if anchor.RemoveAnchorsFromPath(data.Path) == path {
				element = data.Element
			}

			return data.Element, nil
		})).TraverseJSON(); err != nil {
		return nil, err
	}

	return element, nil
}
