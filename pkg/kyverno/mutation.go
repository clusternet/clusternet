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

package kyverno

import (
	"encoding/json"
	"fmt"
	"reflect"

	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/common"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/jmespath"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/mutate"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/response"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/utils"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/variables"
	"github.com/pkg/errors"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

func LoadContext(contextEntries []kyvernov1.KyvernoContextEntry, jsonCtx context.Interface) error {
	for _, entry := range contextEntries {
		if entry.Variable != nil {
			path := ""
			if entry.Variable.JMESPath != "" {
				jp, err := variables.SubstituteAll(jsonCtx, entry.Variable.JMESPath)
				if err != nil {
					return fmt.Errorf("failed to substitute variables in context entry %s/%s, err %s",
						entry.Name, entry.Variable.JMESPath, err.Error())
				}
				path = jp.(string)
			}
			var defaultValue interface{} = nil
			if entry.Variable.Default != nil {
				value, err := variables.DocumentToUntyped(entry.Variable.Default)
				if err != nil {
					return fmt.Errorf("invalid default for variable %s", entry.Name)
				}
				defaultValue, err = variables.SubstituteAll(jsonCtx, value)
				if err != nil {
					return fmt.Errorf("failed to substitute variables in context entry %s %s: %v", entry.Name, entry.Variable.Default, err)
				}
				klog.V(4).Info("evaluated default value", "variable name", entry.Name, "jmespath", defaultValue)
			}
			var output interface{} = defaultValue
			if entry.Variable.Value != nil {
				value, _ := variables.DocumentToUntyped(entry.Variable.Value)
				variable, err := variables.SubstituteAll(jsonCtx, value)
				if err != nil {
					return fmt.Errorf("failed to substitute variables in context entry %s %s: %v", entry.Name, entry.Variable.Value, err)
				}
				if path != "" {
					variable, err := applyJMESPath(path, variable)
					if err == nil {
						output = variable
					} else if defaultValue == nil {
						return fmt.Errorf("failed to apply jmespath %s to variable %s: %v", path, entry.Variable.Value, err)
					}
				} else {
					output = variable
				}
			} else {
				if path != "" {
					if variable, err := jsonCtx.Query(path); err == nil {
						output = variable
					} else if defaultValue == nil {
						return fmt.Errorf("failed to apply jmespath %s to variable %v", path, err)
					}
				}
			}
			klog.V(4).Info("evaluated output", "variable name", entry.Name, "output", output)
			if output == nil {
				return fmt.Errorf("unable to add context entry for variable %s since it evaluated to nil", entry.Name)
			}
			if outputBytes, err := json.Marshal(output); err == nil {
				jsonCtx.ReplaceContextEntry(entry.Name, outputBytes)
			} else {
				return fmt.Errorf("unable to add context entry for variable %s: %w", entry.Name, err)
			}
		}
	}
	return nil
}

func Mutate(originData []byte, configName string, patchConfig *kyvernov1.KyvernoPatchConfig) ([][]byte, error) {
	originUtd, err := utils.ConvertToUnstructured(originData)
	if err != nil {
		return nil, fmt.Errorf("convert data to unstructured failed, err %s", err.Error())
	}

	jsonCtx := context.NewContext()
	if err := jsonCtx.AddVariable("request.object", originUtd); err != nil {
		return nil, err
	}
	if err := jsonCtx.AddImageInfos(originUtd); err != nil {
		return nil, err
	}
	if err := context.MutateResourceWithImageInfo(originData, jsonCtx); err != nil {
		return nil, err
	}

	resource, err := jsonCtx.Query("request.object")
	if err != nil {
		return nil, err
	}
	jsonCtx.Reset()
	if err := jsonCtx.AddResource(resource.(map[string]interface{})); err != nil {
		return nil, err
	}

	if err := LoadContext(patchConfig.Context, jsonCtx); err != nil {
		return nil, err
	}

	klog.V(4).Info("apply override config to resource", "overrideconfig", configName,
		"resource namespace", originUtd.GetNamespace(), "resource name", originUtd.GetName())
	var ruleResp *response.RuleResponse
	if patchConfig.Mutation.ForEachMutation != nil {
		ruleResp = mutateForEach(configName, patchConfig, jsonCtx, *originUtd)
	} else {
		ruleResp = mutateResource(patchConfig, jsonCtx, *originUtd)
	}
	if ruleResp == nil {
		return nil, errors.New("nil rule response")
	}
	if ruleResp.Status == response.RuleStatusPass || ruleResp.Status == response.RuleStatusSkip {
		return ruleResp.Patches, nil
	}
	return nil, errors.New(ruleResp.ToString())
}

func applyJMESPath(jmesPath string, data interface{}) (interface{}, error) {
	jp, err := jmespath.New(jmesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to compile JMESPath: %s, error: %v", jmesPath, err)
	}

	return jp.Search(data)
}

func checkPreconditions(jsonCtx context.Interface, anyAllConditions apiextensions.JSON) (bool, error) {
	preconditions, err := variables.SubstituteAllInPreconditions(jsonCtx, anyAllConditions)
	if err != nil {
		return false, errors.Wrapf(err, "failed to substitute variables in preconditions")
	}

	typeConditions, err := common.TransformConditions(preconditions)
	if err != nil {
		return false, errors.Wrapf(err, "failed to parse preconditions")
	}

	pass := variables.EvaluateConditions(jsonCtx, typeConditions)
	return pass, nil
}

func ruleError(ruleType response.RuleType, msg string, err error) *response.RuleResponse {
	msg = fmt.Sprintf("%s: %s", msg, err.Error())
	return ruleResponse(ruleType, msg, response.RuleStatusError, nil)
}

func ruleResponse(ruleType response.RuleType, msg string, status response.RuleStatus, patchedResource *unstructured.Unstructured) *response.RuleResponse {
	resp := &response.RuleResponse{
		Type:    ruleType,
		Message: msg,
		Status:  status,
	}
	return resp
}

func buildRuleResponse(mutateResp *mutate.Response, patchedResource *unstructured.Unstructured) *response.RuleResponse {
	resp := ruleResponse(response.Mutation, mutateResp.Message, mutateResp.Status, patchedResource)
	if resp.Status == response.RuleStatusPass {
		resp.Patches = mutateResp.Patches
		resp.Message = buildSuccessMessage(mutateResp.PatchedResource)
	}

	return resp
}

func buildSuccessMessage(r unstructured.Unstructured) string {
	if reflect.DeepEqual(unstructured.Unstructured{}, r) {
		return "mutated resource"
	}

	if r.GetNamespace() == "" {
		return fmt.Sprintf("mutated %s/%s", r.GetKind(), r.GetName())
	}

	return fmt.Sprintf("mutated %s/%s in namespace %s", r.GetKind(), r.GetName(), r.GetNamespace())
}

func evaluateList(jmesPath string, ctx context.EvalInterface) ([]interface{}, error) {
	i, err := ctx.Query(jmesPath)
	if err != nil {
		return nil, err
	}

	l, ok := i.([]interface{})
	if !ok {
		return []interface{}{i}, nil
	}

	return l, nil
}

func mutateForEach(configName string, patchConfig *kyvernov1.KyvernoPatchConfig,
	jsonContext context.Interface, resource unstructured.Unstructured) *response.RuleResponse {
	foreachList := patchConfig.Mutation.ForEachMutation
	patchedResource := resource
	var applyCount int
	allPatches := make([][]byte, 0)

	for _, foreach := range foreachList {
		if err := LoadContext(patchConfig.Context, jsonContext); err != nil {
			klog.Error(err, "failed to load context")
			return ruleError(response.Mutation, "failed to load context", err)
		}
		preconditionsPassed, err := checkPreconditions(jsonContext, patchConfig.GetAnyAllConditions())
		if err != nil {
			return ruleError(response.Mutation, "failed to evaluate preconditions", err)
		}

		if !preconditionsPassed {
			return ruleResponse(
				response.Mutation, "preconditions not met", response.RuleStatusSkip, &patchedResource)
		}

		elements, err := evaluateList(foreach.List, jsonContext)
		if err != nil {
			msg := fmt.Sprintf("failed to evaluate list %s", foreach.List)
			return ruleError(response.Mutation, msg, err)
		}

		mutateResp := mutateElements(configName, foreach, jsonContext, elements, patchedResource)
		if mutateResp.Status == response.RuleStatusError || mutateResp.Status == response.RuleStatusFail {
			klog.Error("failed to mutate elements", mutateResp.Status, mutateResp.Message)
			return buildRuleResponse(mutateResp, nil)
		}

		if mutateResp.Status != response.RuleStatusSkip {
			applyCount++
			if len(mutateResp.Patches) > 0 {
				patchedResource = mutateResp.PatchedResource
				allPatches = append(allPatches, mutateResp.Patches...)
			}
		}
	}
	if applyCount == 0 {
		return ruleResponse(response.Mutation, "0 elements processed", response.RuleStatusSkip, &resource)
	}
	r := ruleResponse(response.Mutation, fmt.Sprintf("%d elements processed", applyCount),
		response.RuleStatusPass, &patchedResource)
	r.Patches = allPatches
	return r
}

// invertedElement inverted the order of element for patchStrategicMerge  policies as kustomize patch revering the order of patch resources.
func invertedElement(elements []interface{}) {
	for i, j := 0, len(elements)-1; i < j; i, j = i+1, j-1 {
		elements[i], elements[j] = elements[j], elements[i]
	}
}

func addElementToContext(jsonContext context.Interface, e interface{}, elementIndex int) error {
	data, err := variables.DocumentToUntyped(e)
	if err != nil {
		return err
	}
	if err := jsonContext.AddElement(data, elementIndex); err != nil {
		return errors.Wrapf(err, "failed to add element (%v) to JSON context", e)
	}
	return nil
}

func mutateError(err error, message string) *mutate.Response {
	return &mutate.Response{
		Status:          response.RuleStatusFail,
		PatchedResource: unstructured.Unstructured{},
		Patches:         nil,
		Message:         fmt.Sprintf("failed to add element to context: %v", err),
	}
}

func mutateElements(name string, foreach kyvernov1.KyvernoForEachMutation, jsonContext context.Interface,
	elements []interface{}, resource unstructured.Unstructured) *mutate.Response {
	jsonContext.Checkpoint()
	defer jsonContext.Restore()

	patchedResource := resource
	var allPatches [][]byte
	if foreach.RawPatchStrategicMerge != nil {
		invertedElement(elements)
	}

	for i, e := range elements {
		if e == nil {
			continue
		}
		jsonContext.Reset()
		if err := addElementToContext(jsonContext, e, i); err != nil {
			return mutateError(err, fmt.Sprintf("failed to add element to mutate.foreach[%d].context", i))
		}

		if err := LoadContext(foreach.Context, jsonContext); err != nil {
			return mutateError(err, fmt.Sprintf("failed to load to mutate.foreach[%d].context", i))
		}

		preconditionsPassed, err := checkPreconditions(jsonContext, foreach.AnyAllConditions)
		if err != nil {
			return mutateError(err, fmt.Sprintf("failed to evaluate mutate.foreach[%d].preconditions", i))
		}

		if !preconditionsPassed {
			klog.Info("mutate.foreach.preconditions not met", "elementIndex", i)
			continue
		}

		mutateResp := mutate.ForEach(name, foreach, jsonContext, patchedResource)
		if mutateResp.Status == response.RuleStatusFail || mutateResp.Status == response.RuleStatusError {
			return mutateResp
		}

		if len(mutateResp.Patches) > 0 {
			patchedResource = mutateResp.PatchedResource
			allPatches = append(allPatches, mutateResp.Patches...)
		}
	}

	return &mutate.Response{
		Status:          response.RuleStatusPass,
		PatchedResource: patchedResource,
		Patches:         allPatches,
		Message:         "foreach mutation applied",
	}
}

func mutateResource(
	patchConfig *kyvernov1.KyvernoPatchConfig,
	ctx context.Interface, resource unstructured.Unstructured) *response.RuleResponse {

	preconditionsPassed, err := checkPreconditions(ctx, patchConfig.GetAnyAllConditions())
	if err != nil {
		return ruleError(response.Mutation, "failed to evaluate preconditions", err)
	}

	if !preconditionsPassed {
		return ruleResponse(response.Mutation, "preconditions not met", response.RuleStatusSkip, &resource)
	}

	mutateResp := mutate.Mutate(patchConfig, ctx, resource)
	return buildRuleResponse(mutateResp, &mutateResp.PatchedResource)
}
