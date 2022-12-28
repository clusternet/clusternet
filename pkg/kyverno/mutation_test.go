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
	"strings"
	"testing"

	kyvernov1 "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	jsonutils "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/json"
	jsonpatch "github.com/evanphx/json-patch"
	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func Test_VariableSubstitutionPatchStrategicMerge(t *testing.T) {
	testCases := []struct {
		patchConfigRaw []byte
		resourceRaw    []byte
		expectedPatch  [][]byte
	}{
		{
			resourceRaw: []byte(`{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
				  "name": "check-root-user"
				},
				"spec": {
				  "containers": [
					{
					  "name": "check-root-user",
					  "image": "nginxinc/nginx-unprivileged",
					  "securityContext": {
						"runAsNonRoot": true
					  }
					}
				  ]
				}
			  }`),
			patchConfigRaw: []byte(`{
				"mutate": {
					"patchStrategicMerge": {
						"metadata": {
							"labels": {
								"appname": "{{request.object.metadata.name}}"
							}
						}
					}
				}
			  }`),
			expectedPatch: [][]byte{
				[]byte(`{"op":"add","path":"/metadata/labels","value":{"appname":"check-root-user"}}`)},
		},
		{
			resourceRaw: []byte(`{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
				  "name": "test"
				},
				"spec": {
				  "containers": [
					{
					  "name": "test",
					  "image": "foo/bash:5.0"
					}
				  ]
				}
			  }`),
			patchConfigRaw: []byte(`{
				"mutate": {
					"patchStrategicMerge": {
					  "spec": {
						"containers": [
						  {
							"(name)": "*",
							"image": "{{regex_replace_all('^[^/]+','{{@}}','myregistry.corp.com')}}"
						  }
						]
					  }
					}
				  }
			}`),
			expectedPatch: [][]byte{
				[]byte(
					`{"op":"replace","path":"/spec/containers/0/image","value":"myregistry.corp.com/foo/bash:5.0"}`)},
		},
		{
			resourceRaw: []byte(`{
				"apiVersion": "v1",
				"kind": "Pod",
				"metadata": {
				  "name": "nginx-config-test",
				  "labels": {
					"app.kubernetes.io/managed-by": "Helm"
				  }
				},
				"spec": {
				  "containers": [
					{
					  "image": "nginx:latest",
					  "name": "test-nginx"
					}
				  ]
				}
			  }`),
			patchConfigRaw: []byte(`{
				"preconditions": [
				{
					"key": "{{ request.object.metadata.labels.\"app.kubernetes.io/managed-by\"}}",
					"operator": "Equals",
					"value": "Helm"
				}
				],
				"mutate": {
				"patchStrategicMerge": {
					"metadata": {
					"labels": {
						"my-added-label": "test"
					}
					}
				}
				}
			  }`),
			expectedPatch: [][]byte{
				[]byte(`{"op":"add","path":"/metadata/labels/my-added-label","value":"test"}`)},
		},
	}
	for _, test := range testCases {
		var patchConfig kyvernov1.KyvernoPatchConfig
		err := json.Unmarshal(test.patchConfigRaw, &patchConfig)
		if err != nil {
			t.Error(err)
		}

		retData, err := Mutate(test.resourceRaw, "", &patchConfig)
		assert.NilError(t, err)
		assert.DeepEqual(t, test.expectedPatch, retData)
	}
}

func Test_variableSubstitutionPathNotExist(t *testing.T) {
	resourceRaw := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
		  "name": "check-root-user"
		},
		"spec": {
		  "containers": [
			{
			  "name": "check-root-user",
			  "image": "nginxinc/nginx-unprivileged",
			  "securityContext": {
				"runAsNonRoot": true
			  }
			}
		  ]
		}
	  }`)
	patchConfigRaw := []byte(`{
		"mutate": {
			"patchStrategicMerge": {
				"metadata": {
					"labels": {
						"appname": "{{request.object.metadata.name1}}"
					}
				}
			}
		}
	  }`)
	var patchConfig kyvernov1.KyvernoPatchConfig
	err := json.Unmarshal(patchConfigRaw, &patchConfig)
	if err != nil {
		t.Error(err)
	}

	retData, err := Mutate(resourceRaw, "", &patchConfig)
	assert.Assert(t, strings.Contains(err.Error(), "Unknown key \"name1\""))
	assert.Assert(t, len(retData) == 0)
}

func applyJSONPatches(inputData []byte, patches [][]byte) ([]byte, error) {
	if strings.TrimSpace(string(inputData)) == "" {
		return inputData, nil
	}
	patchBytes := jsonutils.JoinPatches(patches...)
	patchObj, err := jsonpatch.DecodePatch(patchBytes)
	if err != nil {
		return nil, err
	}
	patchedData, err := patchObj.Apply(inputData)
	if err != nil {
		return nil, err
	}
	return patchedData, nil
}

func Test_foreach(t *testing.T) {
	resourceRaw := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
		  "name": "test"
		},
		"spec": {
		  "containers": [
			{
			  "name": "test1",
			  "image": "foo1/bash1:5.0"
			},
			{
			  "name": "test2",
			  "image": "foo2/bash2:5.0"
			},
			{
			  "name": "test3",
			  "image": "foo3/bash3:5.0"
			}
		  ]
		}
	  }`)
	patchConfigRaw := []byte(`{
		"mutate": {
		"foreach": [
			{
			"list": "request.object.spec.containers",
			"patchStrategicMerge": {
				"spec": {
				"containers": [
					{
					"name": "{{ element.name }}",
					"image": "registry.io/{{images.containers.{{element.name}}.path}}:{{images.containers.{{element.name}}.tag}}"
					}
				]
				}
			}
			}
		]
		}
	  }`)
	var patchConfig kyvernov1.KyvernoPatchConfig
	err := json.Unmarshal(patchConfigRaw, &patchConfig)
	assert.NilError(t, err)
	patches, err := Mutate(resourceRaw, "", &patchConfig)
	assert.NilError(t, err)
	outRes, err := applyJSONPatches(resourceRaw, patches)
	assert.NilError(t, err)
	var object map[string]interface{}
	err = json.Unmarshal(outRes, &object)
	assert.NilError(t, err)
	containers, _, err := unstructured.NestedSlice(object, "spec", "containers")
	assert.NilError(t, err)
	for _, c := range containers {
		ctnr := c.(map[string]interface{})
		switch ctnr["name"] {
		case "test1":
			assert.Equal(t, ctnr["image"], "registry.io/foo1/bash1:5.0")
		case "test2":
			assert.Equal(t, ctnr["image"], "registry.io/foo2/bash2:5.0")
		case "test3":
			assert.Equal(t, ctnr["image"], "registry.io/foo3/bash3:5.0")
		}
	}
}

func Test_foreach_element_mutation(t *testing.T) {
	resourceRaw := []byte(`{
		"apiVersion": "v1",
		"kind": "Pod",
		"metadata": {
		  "name": "nginx"
		},
		"spec": {
		  "containers": [
			{
			  "name": "nginx1",
			  "image": "nginx"
			},
			{
			  "name": "nginx2",
			  "image": "nginx"
			}
		  ]
		}
	  }`)
	patchConfigRaw := []byte(`{
		"mutate": {
		"foreach": [
			{
				"list": "request.object.spec.containers",
				"patchStrategicMerge": {
				"spec": {
					"containers": [
					{
						"(name)": "{{ element.name }}",
						"securityContext": {
						"privileged": false
						}
					}
					]
				}
				}
			}
		]
		}
	  }`)
	var patchConfig kyvernov1.KyvernoPatchConfig
	err := json.Unmarshal(patchConfigRaw, &patchConfig)
	assert.NilError(t, err)
	patches, err := Mutate(resourceRaw, "", &patchConfig)
	assert.NilError(t, err)
	outRes, err := applyJSONPatches(resourceRaw, patches)
	assert.NilError(t, err)
	var object map[string]interface{}
	err = json.Unmarshal(outRes, &object)
	assert.NilError(t, err)
	containers, _, err := unstructured.NestedSlice(object, "spec", "containers")
	assert.NilError(t, err)
	for _, c := range containers {
		ctnr := c.(map[string]interface{})
		_securityContext, ok := ctnr["securityContext"]
		assert.Assert(t, ok)

		securityContext := _securityContext.(map[string]interface{})
		assert.Equal(t, securityContext["privileged"], false)
	}
}


