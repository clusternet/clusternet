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

GOOS=${GOOS:-linux}
GOARCH=${GOARCH:-amd64}
CGO_ENABLED=${CGO_ENABLED:-0}

readonly CLUSTERNET_ROOT=$(dirname "${BASH_SOURCE[0]}")/../..

source "${CLUSTERNET_ROOT}/hack/lib/version.sh"

function abspath {
  # run in a subshell for simpler 'cd'
  (
    if [[ -d "${1}" ]]; then # This also catch symlinks to dirs.
      cd "${1}"
      pwd -P
    else
      cd "$(dirname "${1}")"
      local f
      f=$(basename "${1}")
      if [[ -L "${f}" ]]; then
        readlink "${f}"
      else
        echo "$(pwd -P)/${f}"
      fi
    fi
  )
}

clusternet::golang::build_binary() {
	# Create a sub-shell so that we don't pollute the outer environment
	(
		echo "Go version: $(go version)"

		local goldflags
		goldflags="$(clusternet::version::ldflags)"

		local target=$1
		echo "Building cmd/${target} binary for ${GOOS-}/${GOARCH-} ..."

		GOOS=${GOOS} GOARCH=${GOARCH} \
		GOROOT=${GOROOT-} CGO_ENABLED=${CGO_ENABLED-} \
		GOPATH="$( abspath ${CLUSTERNET_ROOT}/../../../../)" \
		go build -ldflags "$goldflags" -o ./_output/${GOOS}/${GOARCH}/bin/${target} ./cmd/${target}/
	)
}

clusternet::docker::image() {
	# Create a sub-shell so that we don't pollute the outer environment
	(
		local target=$1
		tag=$(git describe --tags --always)
		echo "Building docker image clusternet/${target}:${tag} of architecture ${GOARCH} with Go:${GOVERSION} ..."
		docker buildx build --platform ${GOOS}/${GOARCH} \
		  -t clusternet/${target}-${GOARCH}:${tag} \
		  --build-arg BASEIMAGE=${BASEIMAGE} \
		  --build-arg GOVERSION=${GOVERSION} \
		  --build-arg BUILDARCH=${GOARCH} \
		  --build-arg PKGNAME=${target} .
	)
}
