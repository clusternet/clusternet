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

package operator

import (
	"testing"

	"gotest.tools/assert"
)

func TestGetOperatorFromStringPattern_OneChar(t *testing.T) {
	assert.Equal(t, GetOperatorFromStringPattern("f"), Equal)
}

func TestGetOperatorFromStringPattern_EmptyString(t *testing.T) {
	assert.Equal(t, GetOperatorFromStringPattern(""), Equal)
}

func TestGetOperatorFromStringPattern_OnlyOperator(t *testing.T) {
	assert.Equal(t, GetOperatorFromStringPattern(">="), MoreEqual)
}

func TestGetOperatorFromStringPattern_RangeOperator(t *testing.T) {
	assert.Equal(t, GetOperatorFromStringPattern("0-1"), InRange)
	assert.Equal(t, GetOperatorFromStringPattern("0Mi-1024Mi"), InRange)

	assert.Equal(t, GetOperatorFromStringPattern("0!-1"), NotInRange)
	assert.Equal(t, GetOperatorFromStringPattern("0Mi!-1024Mi"), NotInRange)

	assert.Equal(t, GetOperatorFromStringPattern("text1024Mi-2048Mi"), Equal)
	assert.Equal(t, GetOperatorFromStringPattern("test-value"), Equal)
	assert.Equal(t, GetOperatorFromStringPattern("value-*"), Equal)

	assert.Equal(t, GetOperatorFromStringPattern("text1024Mi!-2048Mi"), Equal)
	assert.Equal(t, GetOperatorFromStringPattern("test!-value"), Equal)
	assert.Equal(t, GetOperatorFromStringPattern("value!-*"), Equal)
}
