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

package json

import (
	"encoding/json"

	jsonpatch "github.com/evanphx/json-patch"
)

type PatchOperation struct {
	Path  string      `json:"path"`
	Op    string      `json:"op"`
	Value interface{} `json:"value,omitempty"`
}

func NewPatchOperation(path, op string, value interface{}) PatchOperation {
	return PatchOperation{path, op, value}
}

func (p *PatchOperation) Marshal() ([]byte, error) {
	return json.Marshal(p)
}

func (p *PatchOperation) ToPatchBytes() ([]byte, error) {
	if patch, err := json.Marshal(p); err != nil {
		return nil, err
	} else {
		return JoinPatches(patch), nil
	}
}

func MarshalPatchOperation(path, op string, value interface{}) ([]byte, error) {
	p := NewPatchOperation(path, op, value)
	return p.Marshal()
}

func CheckPatch(patch []byte) error {
	_, err := jsonpatch.DecodePatch([]byte("[" + string(patch) + "]"))
	return err
}

func UnmarshalPatchOperation(patch []byte) (*PatchOperation, error) {
	var p PatchOperation
	if err := json.Unmarshal(patch, &p); err != nil {
		return nil, err
	}
	return &p, nil
}
