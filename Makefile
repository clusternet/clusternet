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

CRD_OPTIONS ?= "crd:crdVersions=v1,allowDangerousTypes=true"

# Constants used throughout.
.EXPORT_ALL_VARIABLES:
BASEIMAGE ?= alpine:3.18.4
GOVERSION ?= 1.21.3
REGISTRY ?= ghcr.io

# Run tests
.PHONY: test
test: generated
	go test -race -coverprofile coverage.out -covermode=atomic ./...

# Generate CRDs
.PHONY: crds
crds: controller-gen
	@echo "Generating CRDs at manifests/crds"
	@$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./pkg/apis/apps/..." output:crd:dir=manifests/crds
	@$(CONTROLLER_GEN) $(CRD_OPTIONS) rbac:roleName=manager-role webhook paths="./pkg/apis/clusters/..." output:crd:dir=manifests/crds

# Verify all changes
.PHONY: verify
verify:
	hack/verify-all.sh

# Run go fmt against code
.PHONY: fmt
fmt:
	@find . -type f -name '*.go'| grep -v "/vendor/" | grep -v "/pkg/generated/" | xargs gofmt -w -s

# Run golang lint against code
.PHONY: lint
lint: golangci-lint
	@$(GOLANG_LINT) --version
	@$(GOLANG_LINT) run

# Run mod tidy against code
.PHONY: tidy
tidy:
	@go mod tidy

# Produce auto-generated files needed for the build.
#
# Example:
#   make generated
.PHONY: generated
generated: controller-gen
	@make crds
	@./hack/update-codegen.sh

# Build Binaries
#
# use WHAT to specify desired targets
# use PLATFORMS to specify desired platforms
# Example:
#   make binaries
#   WHAT=clusternet-agent make binaries
#   WHAT=clusternet-hub,clusternet-agent PLATFORMS=linux/amd64,linux/arm64 make binaries
#   WHAT=clusternet-hub,clusternet-agent,clusternet-scheduler PLATFORMS=linux/amd64,linux/arm64 make binaries
#   PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x,linux/386,linux/arm make binaries
.PHONY: binaries
binaries:
	@hack/make-rules/build.sh

# Build Images
#
# use WHAT to specify desired targets
# use PLATFORMS to specify desired platforms
# Example:
#   make images
#   WHAT=clusternet-agent make images
#   WHAT=clusternet-hub,clusternet-agent PLATFORMS=linux/amd64,linux/arm64 make images
#   WHAT=clusternet-hub,clusternet-agent,clusternet-scheduler PLATFORMS=linux/amd64,linux/arm64 make images
#   PLATFORMS=linux/amd64,linux/arm64,linux/ppc64le,linux/s390x,linux/386,linux/arm make images
.PHONY: images
images:
	@hack/make-rules/images.sh

# find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.14.0 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(shell go env GOPATH)/bin/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif

# find or download golangci-lint
# download golangci-lint if necessary
golangci-lint:
ifeq (, $(shell which golangci-lint))
	@{ \
	set -e ;\
	export GO111MODULE=on; \
	GOLANG_LINT_TMP_DIR=$$(mktemp -d) ;\
	cd $$GOLANG_LINT_TMP_DIR ;\
	go mod init tmp ;\
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.56.2 ;\
	rm -rf $$GOLANG_LINT_TMP_DIR ;\
	}
GOLANG_LINT=$(shell go env GOPATH)/bin/golangci-lint
else
GOLANG_LINT=$(shell which golangci-lint)
endif
