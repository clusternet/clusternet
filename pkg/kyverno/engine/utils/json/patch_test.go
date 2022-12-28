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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPatchOperation(t *testing.T) {
	type args struct {
		path  string
		op    string
		value interface{}
	}
	tests := []struct {
		name string
		args args
		want PatchOperation
	}{{
		name: "test",
		args: args{"path", "op", 123},
		want: PatchOperation{
			Path:  "path",
			Op:    "op",
			Value: 123,
		},
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, NewPatchOperation(tt.args.path, tt.args.op, tt.args.value))
		})
	}
}

func TestPatchOperation_Marshal(t *testing.T) {
	type fields struct {
		Path  string
		Op    string
		Value interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{{
		name: "test",
		fields: fields{
			Path:  "path",
			Op:    "op",
			Value: 123,
		},
		want:    []byte(`{"path":"path","op":"op","value":123}`),
		wantErr: false,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PatchOperation{
				Path:  tt.fields.Path,
				Op:    tt.fields.Op,
				Value: tt.fields.Value,
			}
			got, err := p.Marshal()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestPatchOperation_ToPatchBytes(t *testing.T) {
	type fields struct {
		Path  string
		Op    string
		Value interface{}
	}
	tests := []struct {
		name    string
		fields  fields
		want    []byte
		wantErr bool
	}{{
		name: "test",
		fields: fields{
			Path:  "path",
			Op:    "op",
			Value: 123,
		},
		want:    []byte(`[{"path":"path","op":"op","value":123}]`),
		wantErr: false,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PatchOperation{
				Path:  tt.fields.Path,
				Op:    tt.fields.Op,
				Value: tt.fields.Value,
			}
			got, err := p.ToPatchBytes()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestMarshalPatchOperation(t *testing.T) {
	type args struct {
		path  string
		op    string
		value interface{}
	}
	tests := []struct {
		name    string
		args    args
		want    []byte
		wantErr bool
	}{{
		name: "test",
		args: args{
			path:  "path",
			op:    "op",
			value: 123,
		},
		want:    []byte(`{"path":"path","op":"op","value":123}`),
		wantErr: false,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := MarshalPatchOperation(tt.args.path, tt.args.op, tt.args.value)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestCheckPatch(t *testing.T) {
	type args struct {
		patch []byte
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{{
		name:    "test",
		args:    args{[]byte(`{"path":"path","op":"op","value":123}`)},
		wantErr: false,
	}, {
		name:    "error",
		args:    args{[]byte(`"foo":"bar"`)},
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckPatch(tt.args.patch)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUnmarshalPatchOperation(t *testing.T) {
	type args struct {
		patch []byte
	}
	tests := []struct {
		name    string
		args    args
		want    *PatchOperation
		wantErr bool
	}{{
		name: "test",
		args: args{[]byte(`{"path":"path","op":"op","value":123}`)},
		want: &PatchOperation{
			Path:  "path",
			Op:    "op",
			Value: float64(123),
		},
		wantErr: false,
	}, {
		name:    "error",
		args:    args{[]byte(`"foo":"bar"`)},
		wantErr: true,
	}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := UnmarshalPatchOperation(tt.args.patch)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}
