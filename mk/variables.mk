# ===================================================================================================
# Configuration and Variables
# ===================================================================================================

# Version information
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT_HASH := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
SOURCE_DATE_EPOCH ?= $(shell git log -1 --pretty=%ct 2>/dev/null || date -u +%s)
export SOURCE_DATE_EPOCH
export TZ := UTC
export GOFLAGS := -trimpath -buildvcs=false -mod=vendor
GOTOOLCHAIN ?= go1.25.9
export GOTOOLCHAIN
export GOWORK := off
GO := go
define RESOLVE_GO_BIN_SH
GO_BIN="$$($(GO) env GOROOT)/bin/go"; \
if [ ! -x "$$GO_BIN" ]; then \
	echo "❌ Selected Go toolchain binary not found: $$GO_BIN"; \
	exit 1; \
fi
endef
PYTHON ?= python3
PYTHON_TOOLS_VENV := .venv
PYTHON_TOOLS_BIN := $(PYTHON_TOOLS_VENV)/bin
PYTHON_TOOLS := $(PYTHON_TOOLS_BIN)/python3

# Build configuration
BINARY_NAME := xg2g
BUILD_DIR := bin
BACKEND_DIR := backend
FRONTEND_DIR := frontend
# Artifacts and Temporary Directories
ARTIFACTS_DIR := artifacts
TMP_DIR := tmp
# WebUI Distribution
WEBUI_DIST_DIR := $(BACKEND_DIR)/internal/control/http/dist
# Reproducible build flags
BUILD_FLAGS := -trimpath -buildvcs=false
LDFLAGS := -ldflags "-s -w -buildid= -X 'github.com/ManuGH/xg2g/internal/version.Version=$(VERSION)' -X 'github.com/ManuGH/xg2g/internal/version.Commit=$(COMMIT_HASH)' -X 'github.com/ManuGH/xg2g/internal/version.Date=$(BUILD_DATE)'"
DOCKER_IMAGE := xg2g
DOCKER_REGISTRY ?=
PLATFORMS := linux/amd64
# Keep in sync with backend/scripts/build-ffmpeg.sh.
FFMPEG_VERSION := 8.1
FFMPEG_BASE_IMAGE ?= $(DOCKER_IMAGE)-ffmpeg
FFMPEG_BASE_TAG := $(FFMPEG_BASE_IMAGE):$(FFMPEG_VERSION)

# Coverage thresholds (Locked to Baseline per Governance Policy)
COVERAGE_THRESHOLD := 43
EPG_COVERAGE_THRESHOLD := 85

# Test timeout budgets (STAB-002 fail-closed gating)
GO_TEST_TIMEOUT ?= 10m
GO_TEST_COVER_TIMEOUT ?= 15m
GO_TEST_RACE_TIMEOUT ?= 20m
GO_TEST_IDEMPOTENCY_TIMEOUT ?= 5m

# Tool paths and versions
GOBIN ?= $(shell $(GO) env GOBIN)
GOPATH_BIN := $(shell $(GO) env GOPATH)/bin
TOOL_DIR := $(if $(GOBIN),$(GOBIN),$(GOPATH_BIN))

# Locked Tool Versions (Sourced from tools.go / Baseline Policy)
GOLANGCI_LINT_VERSION := v2.8.0
OAPI_CODEGEN_VERSION := v2.5.1
GOVULNCHECK_VERSION := v1.1.4
SYFT_VERSION := v1.19.0
GRYPE_VERSION := v0.87.0
GOSEC_VERSION := v2.22.1
GOLANGCI_LINT_MODULE := github.com/golangci/golangci-lint/v2/cmd/golangci-lint

# Tool executables
GOLANGCI_LINT := $(TOOL_DIR)/golangci-lint
GOVULNCHECK := $(TOOL_DIR)/govulncheck
OAPI_CODEGEN := $(TOOL_DIR)/oapi-codegen
SYFT := $(TOOL_DIR)/syft
GRYPE := $(TOOL_DIR)/grype
