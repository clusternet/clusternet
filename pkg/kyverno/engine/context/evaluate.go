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
	"fmt"
	"reflect"
	"strings"

	"github.com/clusternet/clusternet/pkg/kyverno/engine/jmespath"
	"github.com/pkg/errors"
	"k8s.io/klog/v2"
)

// Query the JSON context with JMESPATH search path
func (ctx *context) Query(query string) (interface{}, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("invalid query (nil)")
	}
	// compile the query
	queryPath, err := jmespath.New(query)
	if err != nil {
		klog.Error(err, "incorrect query", "query", query)
		return nil, fmt.Errorf("incorrect query %s: %v", query, err)
	}
	// search
	ctx.mutex.RLock()
	defer ctx.mutex.RUnlock()
	var data interface{}
	if err := json.Unmarshal(ctx.jsonRaw, &data); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal context")
	}
	result, err := queryPath.Search(data)
	if err != nil {
		return nil, errors.Wrap(err, "JMESPath query failed")
	}
	return result, nil
}

// HasChanged accepts a JMESPath expression and compares matching data in the
// request.object and request.oldObject context fields. If the data has changed
// it return `true`
func (ctx *context) HasChanged(jmespath string) (bool, error) {
	objData, err := ctx.Query("request.object." + jmespath)
	if err != nil {
		return false, errors.Wrap(err, "failed to query request.object")
	}
	if objData == nil {
		return false, fmt.Errorf("request.object.%s not found", jmespath)
	}
	oldObjData, err := ctx.Query("request.oldObject." + jmespath)
	if err != nil {
		return false, errors.Wrap(err, "failed to query request.object")
	}
	if oldObjData == nil {
		return false, fmt.Errorf("request.oldObject.%s not found", jmespath)
	}
	return !reflect.DeepEqual(objData, oldObjData), nil
}
