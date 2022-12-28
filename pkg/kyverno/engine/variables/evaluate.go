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
	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"github.com/clusternet/clusternet/pkg/kyverno/engine/variables/operator"
	"k8s.io/klog/v2"
)

// Evaluate evaluates the condition
func Evaluate(ctx context.EvalInterface, condition kyvernov1.KyvernoCondition) bool {
	// get handler for the operator
	handle := operator.CreateOperatorHandler(ctx, condition.Operator)
	if handle == nil {
		return false
	}
	return handle.Evaluate(condition.GetKey(), condition.GetValue())
}

// EvaluateConditions evaluates all the conditions present in a slice, in a backwards compatible way
func EvaluateConditions(ctx context.EvalInterface, conditions interface{}) bool {
	switch typedConditions := conditions.(type) {
	case kyvernov1.AnyAllConditions:
		return evaluateAnyAllConditions(ctx, typedConditions)
	case []kyvernov1.KyvernoCondition: // backwards compatibility
		return evaluateOldConditions(ctx, typedConditions)
	}
	return false
}

func EvaluateAnyAllConditions(ctx context.EvalInterface, conditions []kyvernov1.AnyAllConditions) bool {
	for _, c := range conditions {
		if !evaluateAnyAllConditions(ctx, c) {
			return false
		}
	}

	return true
}

// evaluateAnyAllConditions evaluates multiple conditions as a logical AND (all) or OR (any) operation depending on the conditions
func evaluateAnyAllConditions(ctx context.EvalInterface, conditions kyvernov1.AnyAllConditions) bool {
	anyConditions, allConditions := conditions.AnyConditions, conditions.AllConditions
	anyConditionsResult, allConditionsResult := true, true

	// update the anyConditionsResult if they are present
	if anyConditions != nil {
		anyConditionsResult = false
		for _, condition := range anyConditions {
			if Evaluate(ctx, condition) {
				anyConditionsResult = true
				break
			}
		}

		if !anyConditionsResult {
			klog.V(3).Info("no condition passed for 'any' block", "any", anyConditions)
		}
	}

	// update the allConditionsResult if they are present
	for _, condition := range allConditions {
		if !Evaluate(ctx, condition) {
			allConditionsResult = false
			klog.V(3).Info("a condition failed in 'all' block", "condition", condition)
			break
		}
	}

	finalResult := anyConditionsResult && allConditionsResult
	return finalResult
}

// evaluateOldConditions evaluates multiple conditions when those conditions are provided in the old manner i.e. without 'any' or 'all'
func evaluateOldConditions(ctx context.EvalInterface, conditions []kyvernov1.KyvernoCondition) bool {
	for _, condition := range conditions {
		if !Evaluate(ctx, condition) {
			return false
		}
	}

	return true
}
