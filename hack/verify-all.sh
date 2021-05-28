#!/usr/bin/env bash

# Copyright 2021 The Clusternet Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -o errexit
set -o nounset
set -o pipefail

SCRIPT_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

bash "${SCRIPT_ROOT}/hack/verify-codegen.sh"
bash "${SCRIPT_ROOT}/hack/verify-crdgen.sh"
bash "${SCRIPT_ROOT}/hack/verify-gofmt.sh"

echo "verifying if there is any unused dependency in go module"
make -C "${SCRIPT_ROOT}" tidy
STATUS=$(cd "${SCRIPT_ROOT}" && git status --porcelain go.mod go.sum)
if [ ! -z "$STATUS" ]; then
  echo "Running 'go mod tidy' to fix your 'go.mod' and/or 'go.sum'"
  exit 1
fi
echo "go module is tidy."
