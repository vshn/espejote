# Set Shell to bash, otherwise some targets fail with dash/zsh etc.
SHELL := /bin/bash

# Disable built-in rules
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-builtin-variables
.SUFFIXES:
.SECONDARY:
.DEFAULT_GOAL := help

PROJECT_ROOT_DIR = .
include Makefile.vars.mk

.PHONY: help
help: ## Show this help
	@grep -E -h '\s##\s' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

all: build ## Invokes the build target

.PHONY: test
test: ## Run tests
	go test ./... -coverprofile cover.tmp.out
	cat cover.tmp.out | grep -v "zz_generated.deepcopy.go" > cover.out

.PHONY: build
build: generate manifests fmt vet $(BIN_FILENAME) ## Build manager binary

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	go tool sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: ## Generate manifests e.g. CRD, RBAC etc.
	go generate ./...
	go tool sigs.k8s.io/controller-tools/cmd/controller-gen object paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: fmt vet generate manifests ## All-in-one linting
	@echo 'Checking kustomize build ...'
	$(KUSTOMIZE) build config/crd -o /dev/null
	$(KUSTOMIZE) build config/default -o /dev/null
	@echo 'Check for uncommitted changes ...'
	git diff --exit-code

.PHONY: build.docker
build.docker: $(BIN_FILENAME) ## Build the docker image
	docker build . \
		--tag $(GHCR_IMG)

clean: ## Cleans up the generated resources
	rm -rf dist/ cover.out $(BIN_FILENAME) || true

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

###
### Assets
###

# Build the binary without running generators
.PHONY: $(BIN_FILENAME)
$(BIN_FILENAME): export CGO_ENABLED = 0
$(BIN_FILENAME):
	@echo "GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH)"
	go build -o $(BIN_FILENAME)
