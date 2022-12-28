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

	"k8s.io/klog/v2"
)

// AddJSONObject merges json data
func AddJSONObject(ctx Interface, data interface{}) error {
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return ctx.addJSON(jsonBytes)
}

// AddResource add resource to context
func AddResource(ctx Interface, dataRaw []byte) error {
	var data map[string]interface{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		klog.Error(err, "failed to unmarshal the resource")
		return err
	}
	return ctx.AddResource(data)
}

// ReplaceResource replace resource from context
func ReplaceResource(ctx Interface, data map[string]interface{}) error {
	if err := ctx.AddResource(nil); err != nil {
		return err
	}
	return ctx.AddResource(data)
}

// AddOldResource add old resource to context
func AddOldResource(ctx Interface, dataRaw []byte) error {
	var data map[string]interface{}
	if err := json.Unmarshal(dataRaw, &data); err != nil {
		klog.Error(err, "failed to unmarshal the resource")
		return err
	}
	return ctx.AddOldResource(data)
}

// ReplaceOldResource replace old resource from context
func ReplaceOldResource(ctx Interface, data map[string]interface{}) error {
	if err := ctx.AddOldResource(nil); err != nil {
		return err
	}
	return ctx.AddOldResource(data)
}

func addToContext(ctx *context, data interface{}, tags ...string) error {
	dataRaw, err := json.Marshal(push(data, tags...))
	if err != nil {
		klog.Error(err, "failed to marshal the resource")
		return err
	}
	return ctx.addJSON(dataRaw)
}

func push(data interface{}, tags ...string) interface{} {
	for i := len(tags) - 1; i >= 0; i-- {
		data = map[string]interface{}{
			tags[i]: data,
		}
	}
	return data
}
