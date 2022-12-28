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
	"strings"
)

// JoinPatches joins array of serialized JSON patches to the single JSONPatch array
// It accepts patch operations and patches (arrays of patch operations) and returns
// a single combined patch.
func JoinPatches(patches ...[]byte) []byte {
	if len(patches) == 0 {
		return nil
	}

	var patchOperations []string
	for _, patch := range patches {
		str := strings.TrimSpace(string(patch))
		if len(str) == 0 {
			continue
		}

		if strings.HasPrefix(str, "[") {
			str = strings.TrimPrefix(str, "[")
			str = strings.TrimSuffix(str, "]")
			str = strings.TrimSpace(str)
		}

		patchOperations = append(patchOperations, str)
	}

	if len(patchOperations) == 0 {
		return nil
	}

	result := "[" + strings.Join(patchOperations, ", ") + "]"
	return []byte(result)
}
