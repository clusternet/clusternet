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

	jsonpatch "github.com/evanphx/json-patch"
	"gotest.tools/assert"
)

func Test_JoinPatches(t *testing.T) {
	patches := JoinPatches()
	assert.Assert(t, patches == nil, "invalid patch %#v", string(patches))

	patches = JoinPatches([]byte(""))
	assert.Assert(t, patches == nil, "invalid patch %#v", string(patches))

	patches = JoinPatches([]byte(""), []byte(""), []byte(""), []byte(""))
	assert.Assert(t, patches == nil, "invalid patch %#v", string(patches))

	p1 := `{ "op": "replace", "path": "/baz", "value": "boo" }`
	p2 := `{ "op": "add", "path": "/hello", "value": ["world"] }`
	p1p2 := `[
		{ "op": "replace", "path": "/baz", "value": "boo" },
		{ "op": "add", "path": "/hello", "value": ["world"] }
	]`

	patches = JoinPatches([]byte(p1), []byte(p2))
	_, err := jsonpatch.DecodePatch(patches)
	assert.NilError(t, err, "failed to decode patch %s", string(patches))
	if !jsonpatch.Equal([]byte(p1p2), patches) {
		assert.Assert(t, false, "patches are not equal")
	}

	p3 := `{ "op": "remove", "path": "/foo" }`
	p1p2p3 := `[
		{ "op": "replace", "path": "/baz", "value": "boo" },
		{ "op": "add", "path": "/hello", "value": ["world"] },
		{ "op": "remove", "path": "/foo" }
	]`

	patches = JoinPatches([]byte(p1p2), []byte(p3))
	assert.NilError(t, err, "failed to join patches %s", string(patches))

	_, err = jsonpatch.DecodePatch(patches)
	assert.NilError(t, err, "failed to decode patch %s", string(patches))
	if !jsonpatch.Equal([]byte(p1p2p3), patches) {
		assert.Assert(t, false, "patches are not equal %+v %+v", p1p2p3, string(patches))
	}
}
