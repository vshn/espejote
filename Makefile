# Set Shell to bash, otherwise some targets fail with dash/zsh etc.
SHELL := /bin/bash

# Disable built-in rules
MAKEFLAGS += --no-builtin-rules
MAKEFLAGS += --no-builtin-variables
.SUFFIXES:
.SECONDARY:
.DEFAULT_GOAL := help

PROJECT_ROOT_DIR = .

export GOEXPERIMENT = jsonv2

JSONNET_FILES   ?= $(shell find . -type f -not -path './vendor/*' \( -name '*.*jsonnet' -or -name '*.libsonnet' \))

include Makefile.vars.mk

.PHONY: help
help: ## Show this help
	@grep -E -h '\s##\s' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

all: build ## Invokes the build target

.PHONY: completions
completions:
	mkdir -p contrib/completion/bash \
		contrib/completion/fish \
		contrib/completion/zsh
	go run ./tools/completions bash > contrib/completion/bash/espejote
	go run ./tools/completions fish > contrib/completion/fish/espejote
	go run ./tools/completions zsh > contrib/completion/zsh/_espejote

.PHONY: test
test: manifests generate ## Run tests
	KUBEBUILDER_ASSETS="$(shell go tool sigs.k8s.io/controller-runtime/tools/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" go test ./... -race -coverprofile cover.tmp.out
	cat cover.tmp.out | grep -v "zz_generated.deepcopy.go" > cover.out

.PHONY: build
build: generate manifests fmt vet $(BIN_FILENAME) ## Build manager binary

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	go tool sigs.k8s.io/controller-tools/cmd/controller-gen rbac:roleName=manager-role crd:generateEmbeddedObjectMeta=true webhook paths="./..." output:crd:artifacts:config=config/crd/bases

.PHONY: docs
docs: ## Generate documentation
	go tool github.com/elastic/crd-ref-docs --config=crd-ref-docs-config.yaml --source-path=api/v1alpha1 --output-path docs/api.adoc
	FORCE_COLOR=1 go run ./tools/genclidoc ./docs/cli
	go tool github.com/jsonnet-libs/docsonnet controllers/lib/espejote.libsonnet -o docs/lib

.PHONY: generate
generate: ## Generate manifests e.g. CRD, RBAC etc.
	go generate ./...
	go tool sigs.k8s.io/controller-tools/cmd/controller-gen object paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code
	go fmt ./...
	go tool github.com/google/go-jsonnet/cmd/jsonnetfmt -i -- $(JSONNET_FILES)

.PHONY: vet
vet: ## Run go vet against code
	go vet ./...

.PHONY: lint
lint: completions fmt vet generate manifests docs ## All-in-one linting
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
	rm -rf contrib/ dist/ cover.out $(BIN_FILENAME) || true

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go controller

LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

###
### Assets
###

# Build the binary without running generators
.PHONY: $(BIN_FILENAME)
$(BIN_FILENAME): export CGO_ENABLED = 0
$(BIN_FILENAME):
	@echo "GOOS=$$(go env GOOS) GOARCH=$$(go env GOARCH)"
	go build -o $(BIN_FILENAME)
