# Image URL to use all building/pushing image targets
PROTECTOR_IMG ?= cybozu-go/login-protector:dev
EXPORTER_IMG ?= cybozu-go/tty-exporter:dev

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: setup ## Generate role.yaml
	controller-gen rbac:roleName=manager-role paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

# Utilize Kind or modify the e2e tests to load the image locally, enabling compatibility with other vendors.
.PHONY: test-e2e  # Run the e2e tests against a Kind k8s instance that is spun up.
test-e2e: start-kind load-image deploy
	go test ./test/e2e/ -v -ginkgo.v

.PHONY: lint
lint: setup ## Run golangci-lint linter & yamllint
	golangci-lint run

.PHONY: lint-fix
lint-fix: setup ## Run golangci-lint linter and perform fixes
	golangci-lint run --fix

##@ Build

.PHONY: build
build: manifests fmt vet ## Build manager binary.
	go build -o bin/login-protector cmd/login-protector/main.go

.PHONY: run
run: manifests fmt vet ## Run a controller from your host.
	go run ./cmd/login-protector/main.go

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${PROTECTOR_IMG} . --target=login-protector
	$(CONTAINER_TOOL) build -t ${EXPORTER_IMG} . --target=tty-exporter

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${PROTECTOR_IMG}
	$(CONTAINER_TOOL) push ${EXPORTER_IMG}

.PHONY: load-image
load-image: docker-build setup
	kind load docker-image ${PROTECTOR_IMG}
	kind load docker-image ${EXPORTER_IMG}


.PHONY: build-installer
build-installer: manifests setup ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && kustomize edit set image controller=${PROTECTOR_IMG}
	kustomize build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: start-kind
start-kind: setup
	kind create cluster

.PHONY: stop-kind
stop-kind: setup
	kind delete cluster

.PHONY: deploy
deploy: manifests setup ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && kustomize edit set image controller=${PROTECTOR_IMG}
	kustomize build config/default | kubectl apply -f -
	kubectl -n login-protector-system wait --for=condition=available --timeout=180s --all deployments

.PHONY: undeploy
undeploy: setup ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	kustomize build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

##@ Setup

setup:
	aqua install
