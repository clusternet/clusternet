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
	"reflect"
	"testing"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/context"
	"gotest.tools/assert"
)

func Test_variablesub1(t *testing.T) {
	patternMap := []byte(`
	{
		"kind": "ClusterRole",
		"name": "ns-owner-user1",
		"data": {
			"rules": [
				{
					"apiGroups": [
						""
					],
					"resources": [
						"namespaces"
					],
					"verbs": [
						"*"
					],
					"resourceNames": [
						"{{request.object.metadata.name}}"
					]
				}
			]
		}
	}
	`)

	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"name": "temp1"
		}
	}
		`)

	resultMap := []byte(`{"data":{"rules":[{"apiGroups":[""],"resourceNames":["temp"],"resources":["namespaces"],"verbs":["*"]}]},"kind":"ClusterRole","name":"ns-owner-user1"}`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}
	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err != nil {
		t.Error(err)
	}
	resultRaw, err := json.Marshal(patternCopy)
	if err != nil {
		t.Error(err)
	}

	if !reflect.DeepEqual(resultRaw, resultMap) {
		t.Log(string(resultMap))
		t.Log(string(resultRaw))
		t.Error("result does not match")
	}
}

func Test_variableSubstitutionValue(t *testing.T) {

	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"name": "temp1"
		}
	}
		`)
	patternMap := []byte(`
	{
		"spec": {
			"name": "{{request.object.metadata.name}}"
		}
	}
	`)

	resultMap := []byte(`{"spec":{"name":"temp"}}`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err != nil {
		t.Error(err)
	}
	resultRaw, err := json.Marshal(patternCopy)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(resultMap, resultRaw) {
		t.Error("result does not match")
	}
}

func Test_variableSubstitutionValueOperatorNotEqual(t *testing.T) {

	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"name": "temp1"
		}
	}
		`)
	patternMap := []byte(`
	{
		"spec": {
			"name": "!{{request.object.metadata.name}}"
		}
	}
	`)
	resultMap := []byte(`{"spec":{"name":"!temp"}}`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err != nil {
		t.Error(err)
	}
	resultRaw, err := json.Marshal(patternCopy)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(resultMap, resultRaw) {
		t.Log(string(resultRaw))
		t.Log(string(resultMap))
		t.Error("result does not match")
	}
}

func Test_variableSubstitutionValueFail(t *testing.T) {

	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"name": "temp1"
		}
	}
		`)
	patternMap := []byte(`
	{
		"spec": {
			"name": "{{request.object.metadata.name1}}"
		}
	}
	`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err == nil {
		t.Log("expected to fails")
		t.Fail()
	}

}

func Test_variableSubstitutionObject(t *testing.T) {
	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"variable": {
				"var1": "temp1",
				"var2": "temp2",
				"varNested": {
					"var1": "temp1"
				}
			}
		}
	}
	`)
	patternMap := []byte(`
	{
		"spec": {
			"variable": "{{request.object.spec.variable}}"
		}
	}
	`)
	resultMap := []byte(`{"spec":{"variable":{"var1":"temp1","var2":"temp2","varNested":{"var1":"temp1"}}}}`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err != nil {
		t.Error(err)
	}
	resultRaw, err := json.Marshal(patternCopy)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(resultMap, resultRaw) {
		t.Log(string(resultRaw))
		t.Log(string(resultMap))
		t.Error("result does not match")
	}
}

func Test_variableSubstitutionObjectOperatorNotEqualFail(t *testing.T) {
	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"variable": {
				"var1": "temp1",
				"var2": "temp2",
				"varNested": {
					"var1": "temp1"
				}
			}
		}
	}
	`)
	patternMap := []byte(`
	{
		"spec": {
			"variable": "!{{request.object.spec.variable}}"
		}
	}
	`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	patternCopy, err = SubstituteAll(ctx, patternCopy)
	assert.NilError(t, err)

	patternMapCopy, ok := patternCopy.(map[string]interface{})
	assert.Assert(t, ok)

	specInterface, ok := patternMapCopy["spec"]
	assert.Assert(t, ok)

	specMap, ok := specInterface.(map[string]interface{})
	assert.Assert(t, ok)

	variableInterface, ok := specMap["variable"]
	assert.Assert(t, ok)

	variableString, ok := variableInterface.(string)
	assert.Assert(t, ok)

	expected := `!{"var1":"temp1","var2":"temp2","varNested":{"var1":"temp1"}}`
	assert.Equal(t, expected, variableString)
}

func Test_variableSubstitutionMultipleObject(t *testing.T) {
	resourceRaw := []byte(`
	{
		"metadata": {
			"name": "temp",
			"namespace": "n1"
		},
		"spec": {
			"namespace": "n1",
			"variable": {
				"var1": "temp1",
				"var2": "temp2",
				"varNested": {
					"var1": "temp1"
				}
			}
		}
	}
	`)
	patternMap := []byte(`
	{
		"spec": {
			"var": "{{request.object.spec.variable.varNested.var1}}",
			"variable": "{{request.object.spec.variable}}"
		}
	}
	`)

	resultMap := []byte(`{"spec":{"var":"temp1","variable":{"var1":"temp1","var2":"temp2","varNested":{"var1":"temp1"}}}}`)

	var pattern, patternCopy, resource interface{}
	var err error
	err = json.Unmarshal(patternMap, &pattern)
	if err != nil {
		t.Error(err)
	}
	err = json.Unmarshal(patternMap, &patternCopy)
	if err != nil {
		t.Error(err)
	}

	err = json.Unmarshal(resourceRaw, &resource)
	if err != nil {
		t.Error(err)
	}

	// context
	ctx := context.NewContext()
	err = context.AddResource(ctx, resourceRaw)
	if err != nil {
		t.Error(err)
	}

	if patternCopy, err = SubstituteAll(ctx, patternCopy); err != nil {
		t.Error(err)
	}
	resultRaw, err := json.Marshal(patternCopy)
	if err != nil {
		t.Error(err)
	}
	if !reflect.DeepEqual(resultMap, resultRaw) {
		t.Log(string(resultRaw))
		t.Log(string(resultMap))
		t.Error("result does not match")
	}
}
