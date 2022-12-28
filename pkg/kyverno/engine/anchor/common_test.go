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

package anchor

import (
	"testing"

	"gotest.tools/assert"
)

func TestWrappedWithParentheses_StringIsWrappedWithParentheses(t *testing.T) {
	str := "(something)"
	assert.Assert(t, IsConditionAnchor(str))
}

func TestWrappedWithParentheses_StringHasOnlyParentheses(t *testing.T) {
	str := "()"
	assert.Assert(t, IsConditionAnchor(str))
}

func TestWrappedWithParentheses_StringHasNoParentheses(t *testing.T) {
	str := "something"
	assert.Assert(t, !IsConditionAnchor(str))
}

func TestWrappedWithParentheses_StringHasLeftParentheses(t *testing.T) {
	str := "(something"
	assert.Assert(t, !IsConditionAnchor(str))
}

func TestWrappedWithParentheses_StringHasRightParentheses(t *testing.T) {
	str := "something)"
	assert.Assert(t, !IsConditionAnchor(str))
}

func TestWrappedWithParentheses_StringParenthesesInside(t *testing.T) {
	str := "so)m(et(hin)g"
	assert.Assert(t, !IsConditionAnchor(str))
}

func TestWrappedWithParentheses_Empty(t *testing.T) {
	str := ""
	assert.Assert(t, !IsConditionAnchor(str))
}

func TestIsExistenceAnchor_Yes(t *testing.T) {
	assert.Assert(t, IsExistenceAnchor("^(abc)"))
}

func TestIsExistenceAnchor_NoRightBracket(t *testing.T) {
	assert.Assert(t, !IsExistenceAnchor("^(abc"))
}

func TestIsExistenceAnchor_OnlyHat(t *testing.T) {
	assert.Assert(t, !IsExistenceAnchor("^abc"))
}

func TestIsExistenceAnchor_ConditionAnchor(t *testing.T) {
	assert.Assert(t, !IsExistenceAnchor("(abc)"))
}

func TestRemoveAnchorsFromPath_WorksWithAbsolutePath(t *testing.T) {
	newPath := RemoveAnchorsFromPath("/path/(to)/X(anchors)")
	assert.Equal(t, newPath, "/path/to/anchors")
}

func TestRemoveAnchorsFromPath_WorksWithRelativePath(t *testing.T) {
	newPath := RemoveAnchorsFromPath("path/(to)/X(anchors)")
	assert.Equal(t, newPath, "path/to/anchors")
}
