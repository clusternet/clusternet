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

package wildcards

import (
	"reflect"
	"testing"
)

func TestExpandInMetadata(t *testing.T) {
	testExpand(t, map[string]string{"test/*": "*"}, map[string]string{"test/test": "test"},
		map[string]interface{}{"test/test": "*"})

	testExpand(t, map[string]string{"=(test/*)": "test"}, map[string]string{"test/test": "test"},
		map[string]interface{}{"=(test/test)": "test"})

	testExpand(t, map[string]string{"test/*": "*"}, map[string]string{"test/test1": "test1"},
		map[string]interface{}{"test/test1": "*"})
}

func testExpand(t *testing.T, patternMap, resourceMap map[string]string, expectedMap map[string]interface{}) {
	result := replaceWildcardsInMapKeys(patternMap, resourceMap)
	if !reflect.DeepEqual(expectedMap, result) {
		t.Errorf("expected %v but received %v", expectedMap, result)
	}
}
