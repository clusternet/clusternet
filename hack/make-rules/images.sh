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

CLUSTERNET_ROOT=$(dirname "${BASH_SOURCE[0]}")/../..
source "${CLUSTERNET_ROOT}/hack/lib/build.sh"

IFS="," read -ra platforms <<<"${PLATFORMS}"
for img in $(ls -l "${CLUSTERNET_ROOT}/cmd" | grep ^d | awk '{print $9}'); do
	for platform in "${platforms[@]}"; do
		clusternet::docker::image "${platform}" "${img}"
	done
done
