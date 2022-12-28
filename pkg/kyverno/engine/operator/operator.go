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
	"regexp"
)

// Operator is string alias that represents selection operators enum
type Operator string

const (
	// Equal stands for ==
	Equal Operator = ""
	// MoreEqual stands for >=
	MoreEqual Operator = ">="
	// LessEqual stands for <=
	LessEqual Operator = "<="
	// NotEqual stands for !
	NotEqual Operator = "!"
	// More stands for >
	More Operator = ">"
	// Less stands for <
	Less Operator = "<"
	// InRange stands for -
	InRange Operator = "-"
	// NotInRange stands for !-
	NotInRange Operator = "!-"
)

// ReferenceSign defines the operator for anchor reference
const ReferenceSign Operator = "$()"

// GetOperatorFromStringPattern parses opeartor from pattern
func GetOperatorFromStringPattern(pattern string) Operator {
	if len(pattern) < 2 {
		return Equal
	}

	if pattern[:len(MoreEqual)] == string(MoreEqual) {
		return MoreEqual
	}

	if pattern[:len(LessEqual)] == string(LessEqual) {
		return LessEqual
	}

	if pattern[:len(More)] == string(More) {
		return More
	}

	if pattern[:len(Less)] == string(Less) {
		return Less
	}

	if pattern[:len(NotEqual)] == string(NotEqual) {
		return NotEqual
	}

	if match, _ := regexp.Match(`^(\d+(\.\d+)?)([^-]*)!-(\d+(\.\d+)?)([^-]*)$`, []byte(pattern)); match {
		return NotInRange
	}

	if match, _ := regexp.Match(`^(\d+(\.\d+)?)([^-]*)-(\d+(\.\d+)?)([^-]*)$`, []byte(pattern)); match {
		return InRange
	}

	return Equal
}
