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

package patch

import "bytes"

// buffer is a wrapper around a slice of bytes used for JSON
// marshal and unmarshal operations for a strategic merge patch
type buffer struct {
	*bytes.Buffer
}

// UnmarshalJSON writes the slice of bytes to an internal buffer
func (buff buffer) UnmarshalJSON(b []byte) error {
	buff.Reset()

	if _, err := buff.Write(b); err != nil {
		return err
	}
	return nil
}

// MarshalJSON returns the buffered slice of bytes. The returned slice
// is valid for use only until the next buffer modification.
func (buff buffer) MarshalJSON() ([]byte, error) {
	return buff.Bytes(), nil
}
