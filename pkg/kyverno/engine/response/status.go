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

package response

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RuleStatus represents the status of rule execution
type RuleStatus int

// RuleStatusPass is used to report the result of processing a rule.
const (
	// RuleStatusPass indicates that the resources meets the policy rule requirements
	RuleStatusPass RuleStatus = iota
	// RuleStatusFail indicates that the resource does not meet the policy rule requirements
	RuleStatusFail
	// RuleStatusWarn indicates that the resource does not meet the policy rule requirements, but the policy is not scored
	RuleStatusWarn
	// RuleStatusError indicates that the policy rule could not be evaluated due to a processing error, for
	// example when a variable cannot be resolved  in the policy rule definition. Note that variables
	// that cannot be resolved in preconditions are replaced with empty values to allow existence
	// checks.
	RuleStatusError
	// RuleStatusSkip indicates that the policy rule was not selected based on user inputs or applicability, for example
	// when preconditions are not met, or when conditional or global anchors are not satistied.
	RuleStatusSkip
)

func (s *RuleStatus) String() string {
	return toString[*s]
}

var toString = map[RuleStatus]string{
	RuleStatusPass:  "pass",
	RuleStatusFail:  "fail",
	RuleStatusWarn:  "warning",
	RuleStatusError: "error",
	RuleStatusSkip:  "skip",
}

var toID = map[string]RuleStatus{
	"pass":    RuleStatusPass,
	"fail":    RuleStatusFail,
	"warning": RuleStatusWarn,
	"error":   RuleStatusError,
	"skip":    RuleStatusSkip,
}

// MarshalJSON marshals the enum as a quoted json string
func (s *RuleStatus) MarshalJSON() ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "\"%s\"", toString[*s])
	return []byte(b.String()), nil
}

// UnmarshalJSON unmarshals a quoted json string to the enum value
func (s *RuleStatus) UnmarshalJSON(b []byte) error {
	var strVal string
	err := json.Unmarshal(b, &strVal)
	if err != nil {
		return err
	}

	statusVal, err := getRuleStatus(strVal)
	if err != nil {
		return err
	}

	*s = *statusVal
	return nil
}

func getRuleStatus(s string) (*RuleStatus, error) {
	for k, v := range toID {
		if s == k {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("invalid status: %s", s)
}

func (s *RuleStatus) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	if err := unmarshal(&str); err != nil {
		return err
	}

	statusVal, err := getRuleStatus(str)
	if err != nil {
		return err
	}

	*s = *statusVal
	return nil
}
