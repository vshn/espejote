IMG_TAG ?= latest

CURDIR ?= $(shell pwd)
BIN_FILENAME ?= $(CURDIR)/$(PROJECT_ROOT_DIR)/espejote

KUSTOMIZE ?= go tool sigs.k8s.io/kustomize/kustomize/v5

# Image URL to use all building/pushing image targets
GHCR_IMG ?= ghcr.io/vshn/espejote:$(IMG_TAG)

ENVTEST_K8S_VERSION ?= 1.30.3
