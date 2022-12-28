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

import (
	"testing"

	"gotest.tools/assert"
)

func TestMergePatch(t *testing.T) {
	testCases := []struct {
		rawPolicy   []byte
		rawResource []byte
		expected    []byte
	}{
		{
			// condition matches the first element of the array
			rawPolicy: []byte(`{
        "spec": {
          "containers": [
            {
              "(image)": "gcr.io/google-containers/busybox:*"
            }
          ],
          "imagePullSecrets": [
            {
              "name": "regcred"
            }
          ]
        }
      }`),
			rawResource: []byte(`{
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
          "name": "hello"
        },
        "spec": {
          "containers": [
            {
              "name": "hello",
              "image": "gcr.io/google-containers/busybox:latest"
            },
            {
              "name": "hello2",
              "image": "gcr.io/google-containers/busybox:latest"
            },
            {
              "name": "hello3",
              "image": "gcr.io/google-containers/nginx:latest"
            }
          ]
        }
      }`),
			expected: []byte(`{
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
          "name": "hello"
        },
        "spec": {
          "containers": [
            {
              "image": "gcr.io/google-containers/busybox:latest",
              "name": "hello"
            },
            {
              "image": "gcr.io/google-containers/busybox:latest",
              "name": "hello2"
            },
            {
              "image": "gcr.io/google-containers/nginx:latest",
              "name": "hello3"
            }
          ],
          "imagePullSecrets": [
            {
              "name": "regcred"
            }
          ]
        }
      }`),
		},
		{
			// condition matches the third element of the array
			rawPolicy: []byte(`{
        "spec": {
          "containers": [
            {
              "(image)": "gcr.io/google-containers/nginx:*"
            }
          ],
          "imagePullSecrets": [
            {
              "name": "regcred"
            }
          ]
        }
      }`),
			rawResource: []byte(`{
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
          "name": "hello"
        },
        "spec": {
          "containers": [
            {
              "name": "hello",
              "image": "gcr.io/google-containers/busybox:latest"
            },
            {
              "name": "hello2",
              "image": "gcr.io/google-containers/busybox:latest"
            },
            {
              "name": "hello3",
              "image": "gcr.io/google-containers/nginx:latest"
            }
          ]
        }
      }`),
			expected: []byte(`{
        "apiVersion": "v1",
        "kind": "Pod",
        "metadata": {
          "name": "hello"
        },
        "spec": {
          "containers": [
            {
              "image": "gcr.io/google-containers/busybox:latest",
              "name": "hello"
            },
            {
              "image": "gcr.io/google-containers/busybox:latest",
              "name": "hello2"
            },
            {
              "image": "gcr.io/google-containers/nginx:latest",
              "name": "hello3"
            }
          ],
          "imagePullSecrets": [
            {
              "name": "regcred"
            }
          ]
        }
      }`),
		},
	}

	for i, test := range testCases {
		t.Logf("Running test %d...", i+1)
		out, err := strategicMergePatch(string(test.rawResource), string(test.rawPolicy))
		assert.NilError(t, err)
		assert.DeepEqual(t, toJSON(t, test.expected), toJSON(t, out))
	}
}
