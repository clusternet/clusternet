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

package context

import (
	"encoding/json"
	"strings"
	"sync"

	apiutils "github.com/clusternet/clusternet/pkg/kyverno/engine/utils/api"
	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/pkg/errors"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
)

// EvalInterface is used to query and inspect context data
type EvalInterface interface {
	// Query accepts a JMESPath expression and returns matching data
	Query(query string) (interface{}, error)

	// HasChanged accepts a JMESPath expression and compares matching data in the
	// request.object and request.oldObject context fields. If the data has changed
	// it return `true`. If the data has not changed it returns false. If either
	// request.object or request.oldObject are not found, an error is returned.
	HasChanged(jmespath string) (bool, error)
}

// Interface to manage context operations
type Interface interface {
	// AddVariable adds a variable to the context
	AddVariable(key string, value interface{}) error

	// AddContextEntry adds a context entry to the context
	AddContextEntry(name string, dataRaw []byte) error

	// ReplaceContextEntry replaces a context entry to the context
	ReplaceContextEntry(name string, dataRaw []byte) error

	// AddResource merges resource json under request.object
	AddResource(data map[string]interface{}) error

	// AddOldResource merges resource json under request.oldObject
	AddOldResource(data map[string]interface{}) error

	// AddNamespace merges resource json under request.namespace
	AddNamespace(namespace string) error

	// AddElement adds element info to the context
	AddElement(data interface{}, index int) error

	// AddImageInfo adds image info to the context
	AddImageInfo(info apiutils.ImageInfo) error

	// AddImageInfos adds image infos to the context
	AddImageInfos(resource *unstructured.Unstructured) error

	// ImageInfo returns image infos present in the context
	ImageInfo() map[string]map[string]apiutils.ImageInfo

	// Checkpoint creates a copy of the current internal state and pushes it into a stack of stored states.
	Checkpoint()

	// Restore sets the internal state to the last checkpoint, and removes the checkpoint.
	Restore()

	// Reset sets the internal state to the last checkpoint, but does not remove the checkpoint.
	Reset()

	EvalInterface

	// AddJSON  merges the json with context
	addJSON(dataRaw []byte) error
}

// Context stores the data resources as JSON
type context struct {
	mutex              sync.RWMutex
	jsonRaw            []byte
	jsonRawCheckpoints [][]byte
	images             map[string]map[string]apiutils.ImageInfo
}

// NewContext returns a new context
func NewContext() Interface {
	return NewContextFromRaw([]byte(`{}`))
}

// NewContextFromRaw returns a new context initialized with raw data
func NewContextFromRaw(raw []byte) Interface {
	ctx := context{
		jsonRaw:            raw,
		jsonRawCheckpoints: make([][]byte, 0),
	}
	return &ctx
}

// addJSON merges json data
func (ctx *context) addJSON(dataRaw []byte) error {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	json, err := jsonpatch.MergeMergePatches(ctx.jsonRaw, dataRaw)
	if err != nil {
		return errors.Wrap(err, "failed to merge JSON data")
	}
	ctx.jsonRaw = json
	return nil
}

// AddRequest adds an admission request to context
func (ctx *context) AddRequest(request *admissionv1.AdmissionRequest) error {
	return addToContext(ctx, request, "request")
}

func (ctx *context) AddVariable(key string, value interface{}) error {
	return addToContext(ctx, value, strings.Split(key, ".")...)
}

func (ctx *context) AddContextEntry(name string, dataRaw []byte) error {
	var data interface{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		klog.Error(err, "failed to unmarshal the resource")
		return err
	}
	return addToContext(ctx, data, name)
}

func (ctx *context) ReplaceContextEntry(name string, dataRaw []byte) error {
	var data interface{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		klog.Error(err, "failed to unmarshal the resource")
		return err
	}
	// Adding a nil entry to clean out any existing data in the context with the entry name
	if err := addToContext(ctx, nil, name); err != nil {
		klog.Error(err, "unable to replace context entry", "context entry name", name)
		return err
	}
	return addToContext(ctx, data, name)
}

// AddResource data at path: request.object
func (ctx *context) AddResource(data map[string]interface{}) error {
	return addToContext(ctx, data, "request", "object")
}

// AddOldResource data at path: request.oldObject
func (ctx *context) AddOldResource(data map[string]interface{}) error {
	return addToContext(ctx, data, "request", "oldObject")
}

// AddOperation data at path: request.operation
func (ctx *context) AddOperation(data string) error {
	return addToContext(ctx, data, "request", "operation")
}

// AddNamespace merges resource json under request.namespace
func (ctx *context) AddNamespace(namespace string) error {
	return addToContext(ctx, namespace, "request", "namespace")
}

func (ctx *context) AddElement(data interface{}, index int) error {
	data = map[string]interface{}{
		"element":      data,
		"elementIndex": index,
	}
	return addToContext(ctx, data)
}

func (ctx *context) AddImageInfo(info apiutils.ImageInfo) error {
	data := map[string]interface{}{
		"reference":        info.String(),
		"referenceWithTag": info.ReferenceWithTag(),
		"registry":         info.Registry,
		"path":             info.Path,
		"name":             info.Name,
		"tag":              info.Tag,
		"digest":           info.Digest,
	}
	return addToContext(ctx, data, "image")
}

func (ctx *context) AddImageInfos(resource *unstructured.Unstructured) error {
	images, err := apiutils.ExtractImagesFromResource(*resource)
	if err != nil {
		return err
	}
	if len(images) == 0 {
		return nil
	}
	ctx.images = images

	klog.V(4).Info("updated image info", "images", images)
	return addToContext(ctx, images, "images")
}

func (ctx *context) ImageInfo() map[string]map[string]apiutils.ImageInfo {
	return ctx.images
}

// Checkpoint creates a copy of the current internal state and
// pushes it into a stack of stored states.
func (ctx *context) Checkpoint() {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	jsonRawCheckpoint := make([]byte, len(ctx.jsonRaw))
	copy(jsonRawCheckpoint, ctx.jsonRaw)
	ctx.jsonRawCheckpoints = append(ctx.jsonRawCheckpoints, jsonRawCheckpoint)
}

// Restore sets the internal state to the last checkpoint, and removes the checkpoint.
func (ctx *context) Restore() {
	ctx.reset(true)
}

// Reset sets the internal state to the last checkpoint, but does not remove the checkpoint.
func (ctx *context) Reset() {
	ctx.reset(false)
}

func (ctx *context) reset(remove bool) {
	ctx.mutex.Lock()
	defer ctx.mutex.Unlock()
	if len(ctx.jsonRawCheckpoints) == 0 {
		return
	}
	n := len(ctx.jsonRawCheckpoints) - 1
	jsonRawCheckpoint := ctx.jsonRawCheckpoints[n]
	ctx.jsonRaw = make([]byte, len(jsonRawCheckpoint))
	copy(ctx.jsonRaw, jsonRawCheckpoint)
	if remove {
		ctx.jsonRawCheckpoints = ctx.jsonRawCheckpoints[:n]
	}
}
