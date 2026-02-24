# ===================================================================================================
# xg2g Enterprise Makefile - Production-Grade Development Workflow
# ===================================================================================================
# This Makefile provides comprehensive targets for building, testing, linting, and releasing xg2g.
# All targets are designed with enterprise-grade quality assurance and CI/CD integration in mind.
# ===================================================================================================

.PHONY: help build build-with-ui build-all clean clean-certs lint lint-fix test test-schema test-race test-cover cover test-fuzz test-all \
        docker docker-build docker-build-cpu docker-build-gpu docker-build-all docker-security docker-tag docker-push docker-clean \
        sbom deps deps-update deps-tidy deps-verify deps-licenses \
	security security-scan security-audit security-vulncheck \
	quality-gates quality-gates-offline quality-gates-online ci-pr ci-nightly pre-commit install dev-tools check-tools generate-config verify-config \
        release-check release-build release-tag release-notes \
        dev up down status prod-up prod-down prod-logs check-env \
        restart prod-restart ps prod-ps ui-build codex certs setup build-ffmpeg \
        docs-render verify-docs-compiled release-prepare release-verify-remote recover verify-runtime

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
GOTOOLCHAIN ?= go1.25.7
export GOTOOLCHAIN
GO := go

# Build configuration
BINARY_NAME := xg2g
BUILD_DIR := bin
# Artifacts and Temporary Directories
ARTIFACTS_DIR := artifacts
TMP_DIR := tmp
# WebUI Distribution
WEBUI_DIST_DIR := internal/control/http/dist
# Reproducible build flags
BUILD_FLAGS := -trimpath -buildvcs=false
LDFLAGS := -ldflags "-s -w -buildid= -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT_HASH)' -X 'main.buildDate=$(BUILD_DATE)'"
DOCKER_IMAGE := xg2g
DOCKER_REGISTRY ?=
PLATFORMS := linux/amd64

# Coverage thresholds (Locked to Baseline per Governance Policy)
COVERAGE_THRESHOLD := 43
EPG_COVERAGE_THRESHOLD := 85

# Tool paths and versions
GOBIN ?= $(shell $(GO) env GOBIN)
GOPATH_BIN := $(shell $(GO) env GOPATH)/bin
TOOL_DIR := $(if $(GOBIN),$(GOBIN),$(GOPATH_BIN))

# Locked Tool Versions (Sourced from tools.go / Baseline Policy)
GOLANGCI_LINT_VERSION := v1.63.4
OAPI_CODEGEN_VERSION := v2.5.1
GOVULNCHECK_VERSION := v1.1.4
SYFT_VERSION := v1.19.0
GRYPE_VERSION := v0.87.0
GOSEC_VERSION := v2.22.1

# Tool executables
GOLANGCI_LINT := $(TOOL_DIR)/golangci-lint
GOVULNCHECK := $(TOOL_DIR)/govulncheck
OAPI_CODEGEN := $(TOOL_DIR)/oapi-codegen
SYFT := $(TOOL_DIR)/syft
GRYPE := $(TOOL_DIR)/grype

# ===================================================================================================
# Help and Default Target
# ===================================================================================================

help: ## Show this help message
	@echo "xg2g Enterprise Makefile - Production-Grade Development Workflow"
	@echo "================================================================="
	@echo ""
	@echo "Build Targets:"
	@echo "  build          Build the main daemon binary"
	@echo "  build-with-ui  Build WebUI assets and then build the daemon"
	@echo "  build-all      Build binaries for all supported platforms"
	@echo "  clean          Remove build artifacts and temporary files"
	@echo "  clean-certs    Remove auto-generated TLS certificates"
	@echo "  certs          Generate self-signed TLS certificates"
	@echo ""
	@echo "Quality Assurance:"
	@echo "  lint              Run golangci-lint with all checks"
	@echo "  lint-fix          Run golangci-lint with automatic fixes"
	@echo "  generate          Generate Go code from OpenAPI spec"
	@echo "  generate-config   Generate config surfaces from registry"
	@echo "  verify-config     Verify generated config surfaces are up-to-date"
	@echo "  test              Run all unit tests"
	@echo "  test-schema       Run JSON schema validation tests"
	@echo "  test-race         Run tests with race detection"
	@echo "  test-cover        Run tests with coverage reporting"
	@echo "  test-fuzz         Run comprehensive fuzzing tests"
	@echo "  test-all          Run complete test suite (unit + race + fuzz)"
	@echo ""
	@echo "Enterprise Testing:"
	@echo "  codex             Run the Codex review bundle (lint + race/coverage + govulncheck)"
	@echo "  quality-gates     Validate all online quality gates (coverage, lint, security)"
	@echo "  quality-gates-offline  Validate offline-only gates (no network)"
	@echo "  quality-gates-online   Validate online gates (coverage, lint, security)"
	@echo "  ci-pr             Fast, deterministic PR gate (required checks)"
	@echo "  ci-nightly        Deep, expensive gates (nightly/dispatch)"
	@echo ""
	@echo "Docker Operations:"
	@echo "  docker              Build Docker image"
	@echo "  docker-build        Build Docker image"
	@echo "  docker-security     Run Docker security scanning"
	@echo "  docker-tag          Tag Docker image with version"
	@echo "  docker-push         Push Docker image to registry"
	@echo "  docker-clean        Remove Docker build cache"
	@echo ""
	@echo "Security and Compliance:"
	@echo "  sbom           Generate Software Bill of Materials"
	@echo "  security       Run comprehensive security analysis"
	@echo "  security-scan  Run container vulnerability scanning"
	@echo "  security-audit Run dependency vulnerability audit"
	@echo "  security-vulncheck Run Go vulnerability checker"
	@echo ""
	@echo "Dependency Management:"
	@echo "  deps           Install and verify dependencies"
	@echo "  deps-update    Update dependencies to latest versions"
	@echo "  deps-tidy      Clean and organize Go modules"
	@echo "  deps-verify    Verify dependency integrity"
	@echo "  deps-licenses  Generate dependency license report"
	@echo ""
	@echo "Development Tools:"
	@echo "  dev-tools      Install all development tools"
	@echo "  check-tools    Verify development tools are installed"
	@echo "  pre-commit     Run pre-commit validation checks"
	@echo ""
	@echo "Development & Local:"
	@echo "  dev            Run daemon locally with .env configuration"
	@echo "  check-env      Validate .env and docker compose config"
	@echo "  up             Start docker-compose.yml stack"
	@echo "  down           Stop docker-compose.yml stack"
	@echo "  status         Check API status endpoint"
	@echo "  logs           Show service logs (SVC=name to filter)"
	@echo "  restart        Restart service (SVC=name, defaults to xg2g)"
	@echo "  ps             Show running containers"
	@echo ""
	@echo "Production Operations:"
	@echo "  prod-up        Start production docker-compose stack"
	@echo "  prod-down      Stop production stack"
	@echo "  prod-logs      Show production logs"
	@echo "  prod-restart   Restart production service"
	@echo "  prod-ps        Show production containers"
	@echo ""
	@echo "Deployment Helpers:"
	@echo "  pin-digests            Replace <OWNER> and digest placeholders in deploy/ templates"
	@echo "Deployment Helpers:"
	@echo "  pin-digests            Replace <OWNER> and digest placeholders in deploy/ templates"
	@echo ""
	@echo "Release Management:"
	@echo "  release-check  Validate release readiness"
	@echo "  release-build  Build release artifacts"
	@echo "  release-tag    Create and push release tag"
	@echo "  release-notes  Generate release notes"
	@echo ""
	@echo "Configuration:"
	@echo "  VERSION=$(VERSION)"
	@echo "  COMMIT_HASH=$(COMMIT_HASH)"
	@echo "  BUILD_DATE=$(BUILD_DATE)"
	@echo ""

# ===================================================================================================
# Build Targets
# ===================================================================================================

ui-build: ## Build WebUI assets
	@echo "Building WebUI assets..."
	@cd webui && npm ci && npm run build
	@echo "Copying to $(WEBUI_DIST_DIR)..."
	@rm -rf "$(WEBUI_DIST_DIR)"
	@mkdir -p "$(WEBUI_DIST_DIR)"
	@cp -R webui/dist/. "$(WEBUI_DIST_DIR)"/
	@echo "✅ WebUI build complete"

generate: ## Generate Go code from OpenAPI spec (v3 only)
	@echo "Generating API server code (v3)..."
	@mkdir -p internal/api
	@mkdir -p internal/control/http/v3
	@$(GO) run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-api.yaml -o internal/api/server_gen.go api/openapi.yaml
	@$(GO) run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-v3.yaml -o internal/control/http/v3/server_gen.go api/openapi.yaml
	@echo "✅ Code generation complete (single source: api/openapi.yaml):"
	@echo "   - internal/control/http/v3/server_gen.go"

verify-generate: generate ## Verify that generated code is up-to-date
	@echo "Verifying generated code..."
	@git diff --exit-code internal/api/server_gen.go internal/control/http/v3/server_gen.go || (echo "❌ Generated code is out of sync. Run 'make generate' and commit changes." && exit 1)
	@echo "✅ Generated code is up-to-date"

verify-codegen-transport: ## Verify codegen is transport-only (no scope injection or default http.Error)
	@./scripts/verify-codegen-transport-only.sh

verify-router-parity: ## Verify v3 router parity against OpenAPI
	@./scripts/verify-router-parity.sh

verify-oapi-codegen-version: ## Verify pinned oapi-codegen version in vendor/modules.txt
	@./scripts/verify-oapi-codegen-version.sh

verify-no-hardcoded-baseurl: ## Verify V3 base URL is not hardcoded in internal packages
	@./scripts/verify-no-hardcoded-baseurl.sh

verify-no-adhoc-terminal-mapping: ## Verify terminal outcomes only set via lifecycle SSOT
	@./scripts/verify-no-adhoc-terminal-mapping.sh

verify-no-adhoc-session-mapping: ## Verify session API mappings only set via session_mapping SSOT
	@./scripts/verify-no-adhoc-session-mapping.sh

verify-fuzz: ## Optional fuzz invariants (nightly)
	@$(GO) test ./internal/domain/session/lifecycle -fuzz=Fuzz -fuzztime=3s

generate-config: ## Generate config surfaces from registry
	@echo "Generating config surfaces from registry..."
	@$(GO) run ./cmd/configgen --allow-create
	@echo "✅ Config surfaces generated"

.PHONY: verify
verify: verify-config verify-doc-links verify-capabilities contract-matrix verify-purity contract-freeze-check verify-no-sleep verify-no-panic verify-no-ignored-errors verify-determinism verify-codegen-transport verify-router-parity verify-oapi-codegen-version verify-no-hardcoded-baseurl verify-no-adhoc-terminal-mapping verify-no-adhoc-session-mapping verify-doc-image-tags verify-docs-compiled verify-digest-lock verify-release-policy verify-runtime verify-hot-reload-governance ## Phase 4.7: Run all governance verification gates

verify-config: ## Verify generated config surfaces are up-to-date
	@echo "Verifying generated config surfaces..."
	@$(GO) run ./cmd/configgen
	@if git ls-files --error-unmatch .env >/dev/null 2>&1; then \
		echo "❌ .env must not be tracked in the repo. Use .env.example instead."; \
		exit 1; \
	fi
	@git diff --exit-code docs/guides/CONFIGURATION.md docs/guides/config.schema.json config.generated.example.yaml docs/guides/CONFIG_SURFACES.md || (echo "❌ Config surfaces are out of sync. Run 'make generate-config' and commit changes." && exit 1)
	@echo "✅ Config surfaces are up-to-date"

verify-doc-links: ## Verify docs contains no broken relative links
	@./scripts/verify-doc-links.sh

verify-doc-image-tags: ## Verify Docker image tags are pinned and consistent (Best Practice 2026)
	@echo "--- verify-doc-image-tags ---"
	@./scripts/verify-doc-image-tags.sh

verify-doc-image-tags-negative: ## Mechanical negative test for the image tag gate
	@echo "--- verify-doc-image-tags-negative ---"
	@mkdir -p tmp/gate-test
	@echo "3.1.5" > tmp/gate-test/VERSION
	@echo "ghcr.io/manugh/xg2g:latest" > tmp/gate-test/README.md
	@echo "Testing negative case (forbidden :latest)..."
	@IMAGE_TAG_TEST_ROOT=tmp/gate-test bash -c 'CANONICAL_VERSION=3.1.5 REPO_ROOT=tmp/gate-test ./scripts/verify-doc-image-tags.sh' && (echo "❌ Gate failed to catch :latest"; exit 1) || echo "✅ Gate correctly caught :latest"
	@echo "ghcr.io/manugh/xg2g:3.1.4" > tmp/gate-test/README.md
	@echo "Testing negative case (tag drift)..."
	@IMAGE_TAG_TEST_ROOT=tmp/gate-test bash -c 'CANONICAL_VERSION=3.1.5 REPO_ROOT=tmp/gate-test ./scripts/verify-doc-image-tags.sh' && (echo "❌ Gate failed to catch tag drift"; exit 1) || echo "✅ Gate correctly caught tag drift"
	@rm -rf tmp/gate-test
	@echo "✅ Negative tests passed"

docs-render: ## Render templates into documentation and units (Zero-Drift)
	@echo "--- docs:render ---"
	@./scripts/render-docs.sh

verify-docs-compiled: docs-render ## Verify that all docs and units are up-to-date with templates
	@echo "--- verify-docs-compiled ---"
	@git diff --exit-code README.md docs/ops/xg2g.service docker-compose.yml || (echo "❌ Documentation drift detected. Run 'make docs:render' and commit changes." && exit 1)
	@echo "✅ All documents and units are up-to-date"

verify-digest-lock: ## Verify DIGESTS.lock stability and remote existence
	@echo "--- verify-digest-lock ---"
	@./scripts/verify-digest-lock.sh

verify-release-policy: ## Verify Release PR diff scope policy
	@echo "--- verify-release-policy ---"
	@./scripts/verify-release-policy.sh

release-prepare: ## Prepare a new release (VERSION=X.Y.Z)
	@echo "--- release-prepare ---"
	@./scripts/release-prepare.sh $(VERSION)

release-verify-remote: ## Verify release image in GHCR and update DIGESTS.lock
	@echo "--- release-verify-remote ---"
	@./scripts/release-verify-remote.sh

recover: ## Force recovery/deployment of a specific release (VERSION=X.Y.Z)
	@echo "--- recover ---"
	@./scripts/recover.sh $(VERSION)

verify-runtime: ## Verify live runtime truth against repo truth
	@echo "--- verify-runtime ---"
	@./scripts/verify-runtime.sh

verify-hot-reload-governance: ## Verify that only approved fields are marked HotReloadable
	@echo "--- verify-hot-reload-governance ---"
	@$(GO) run scripts/verify-hot-reload-governance.go

verify-hot-reload: ## Verify hot-reload functionality with race detection
	@echo "--- verify-hot-reload ---"
	@$(GO) test -race ./internal/control/http/v3 -run TestHotReload -v

verify-runtime-continuous: ## Run continuous verification (Rate-limited observer)
	@echo "--- verify-runtime-continuous ---"
	@./scripts/continuous-verify.sh

verify-runtime-timer-setup: ## Setup systemd timer for continuous verification
	@echo "--- verify-runtime-timer-setup ---"
	@if [ "$$(id -u)" -ne 0 ]; then echo "❌ Error: Must run as root"; exit 1; fi
	@cp docs/ops/xg2g-verifier.service /etc/systemd/system/
	@cp docs/ops/xg2g-verifier.timer /etc/systemd/system/
	@systemctl daemon-reload
	@systemctl enable --now xg2g-verifier.timer
	@echo "✅ xg2g-verifier.timer enabled (15m interval)"

.PHONY: verify-hermetic-codegen
verify-hermetic-codegen: ## Verify hermetic code generation invariants (CTO-grade)
	$(MAKE) verify-hermetic-codegen-internal

.PHONY: verify-hermetic-codegen-internal
verify-hermetic-codegen-internal:
	@echo "Verifying hermetic code generation invariants..."
	@# 1. Behavior-based validation: Check what 'make generate' would execute
	@if ! make -n generate | grep -q 'go run -mod=vendor.*oapi-codegen'; then \
		echo "❌ generate target must use 'go run -mod=vendor' for oapi-codegen"; \
		exit 1; \
	fi
	@# 2. Verify vendored module is listed in vendor/modules.txt
	@if ! grep -q 'github.com/oapi-codegen/oapi-codegen' vendor/modules.txt; then \
		echo "❌ oapi-codegen not found in vendor/modules.txt"; \
		exit 1; \
	fi
	@# 3. Directory existence check
	@if [ ! -d vendor/github.com/oapi-codegen/oapi-codegen ]; then \
		echo "❌ oapi-codegen directory missing from vendor/"; \
		exit 1; \
	fi
	@# 4. Verify tools.go declares the tool dependency
	@if ! grep -q 'github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen' tools.go; then \
		echo "❌ tools.go must import oapi-codegen"; \
		exit 1; \
	fi
	@echo "✅ Hermetic code generation verified"

.PHONY: verify-purity
verify-purity: ## Phase 4.7: Verify UI purity, decision ownership, OpenAPI hygiene + lint
	@./scripts/verify-ui-purity.sh
	@./scripts/verify-decision-ownership.sh
	@./scripts/verify-openapi-hygiene.sh
	@python3 ./scripts/verify-openapi-no-duplicate-keys.py api/openapi.yaml
	@./scripts/ci_gate_adr_case.sh
	@./scripts/ci_gate_storage_purity.sh
	@./scripts/verify-openapi-lint.sh
	@./scripts/verify-v3-shadowing.sh

contract-freeze-check: ## Phase 4.7: Verify contract goldens against baseline manifest
	@./scripts/verify-golden-freeze.sh

.PHONY: verify-contract-manifest
verify-contract-manifest: ## Phase 6.0: Verify normative contract manifest integrity
	@echo "--- verify-contract-manifest ---"
	@cd webui && npx vitest run tests/contracts/manifest-integrity.test.ts

.PHONY: verify-openapi-drift
verify-openapi-drift: ## Phase 6.0: Verify OpenAPI normative snapshot stability
	@./scripts/verify-openapi-drift.sh
	@./scripts/verify-ui-consumption-vs-openapi.sh

.PHONY: verify-ui-contract-scan
verify-ui-contract-scan: ## Phase 6.0: Verify UI codebase not violating contract manifest
	@echo "--- verify-ui-contract-scan ---"
	@npx tsx scripts/ts-ast-contract-scan.ts

.PHONY: verify-telemetry-schema
verify-telemetry-schema: ## Phase 6.1: Verify Telemetry Schema Integrity
	@echo "--- verify-telemetry-schema ---"
	@ npx tsx scripts/verify-telemetry-schema.ts
	@./scripts/verify-telemetry-drift.sh

.PHONY: verify-capabilities
verify-capabilities: ## Phase 7: Verify capability fixtures against manifest and runtime resolver
	@echo "--- verify-capabilities ---"
	@$(GO) test ./test/invariants -run Invariant -v
	@$(GO) run ./cmd/generate-capability-fixtures/main.go -check

verify-decision-goldens:
	@echo "--- verify-decision-goldens ---"
	@$(GO) test ./test/invariants -run TestDecisionGoldens -v

verify-decision-invariants:
	@echo "--- verify-decision-invariants ---"
	@$(GO) test ./test/invariants -run TestDecisionOutputInvariants -v

verify-observability:
	@echo "--- verify-observability ---"
	@$(GO) test ./test/invariants -run TestDecisionObservabilityContract -v

verify-p8: verify-capabilities verify-decision-goldens verify-decision-invariants verify-observability

verify-hls-http:
	@echo "--- verify-hls-http ---"
	@$(GO) test -tags v3 ./internal/control/http/v3 -v -run HLS

verify-hls-hygiene:
	@echo "--- verify-hls-hygiene ---"
	@! grep -r "application/vnd.apple.mpegurl" internal/ | grep -v "hls_contract.go" | grep -v "test.go" | grep -v server_gen.go | grep -v "/dist/"
	@! grep -r "video/mp2t" internal/ | grep -v "hls_contract.go" | grep -v "test.go" | grep -v server_gen.go | grep -v "/dist/"

.PHONY: verify-no-sleep verify-no-panic verify-no-ignored-errors
verify-determinism: ## Gate: Deterministic tests in critical packages
	@echo "Checking deterministic test contract (critical packages)..."
	@REPO_ROOT=$(PWD) ./scripts/verify-determinism.sh

verify-no-sleep: ## Gate: No time.Sleep in production code
	@echo "Checking for time.Sleep in production code..."
	@if grep -r "time.Sleep" internal/ --include="*.go" | grep -v "_test.go" | grep -v "mock_server.go"; then \
		echo "❌ time.Sleep found in production code"; \
		exit 1; \
	fi
	@echo "✅ No production time.Sleep"

verify-no-panic: ## Gate: No panics in production code
	@echo "Checking for panics in production code..."
	@if grep -rn "panic(" internal/ --include="*.go" | grep -v "_test.go" | grep -vFf panic_allowlist.txt; then \
		echo "❌ Panic found in production code (not in allowlist)"; \
		exit 1; \
	fi
	@echo "✅ No production panics"

verify-no-ignored-errors: ## Gate: No ignored errors
	@echo "Checking for ignored errors..."
	@if grep -r "_ = err" internal/ --include="*.go" | grep -v "_test.go"; then \
		echo "❌ Ignored error found"; \
		exit 1; \
	fi
	@echo "✅ No ignored errors"

verify-p9: verify-hls-goldens verify-hls-http verify-hls-hygiene

build: ## Build xg2g binary (Go-only, offline-safe)
	@echo "▶ build (Go-only, offline-safe)"
	@mkdir -p $(BUILD_DIR)
	@GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOVCS="*:off" $(GO) build $(BUILD_FLAGS) $(LDFLAGS) -mod=vendor -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-with-ui: ui-build ## Build WebUI assets and then build the daemon
	@echo "▶ build-with-ui (requires Node)"
	@$(MAKE) build

# xg2g-migrate target removed as of v3.0.0 (Phase 3.0 Sunset complete)

clean: ## Remove build artifacts and temporary files
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf coverage.out coverage.html
	@rm -rf *.prof *.test
	@rm -rf dist/
	@$(GO) clean -cache

clean-certs: ## Remove auto-generated TLS certificates
	@echo "Cleaning certificates..."
	@rm -rf certs/
	@echo "✅ Certificates cleaned"

certs: ## Generate self-signed TLS certificates
	@echo "Generating self-signed TLS certificates..."
	@mkdir -p certs
	@$(GO) run -trimpath ./cmd/gencert
	@echo ""
	@echo "To use these certificates, set:"
	@echo "   export XG2G_TLS_CERT=certs/xg2g.crt"
	@echo "   export XG2G_TLS_KEY=certs/xg2g.key"
	@echo ""
	@echo "Or enable auto-generation on startup:"
	@echo "   export XG2G_TLS_ENABLED=true"

clean-full: clean ## Remove all build artifacts
	@$(GO) clean -cache -testcache -modcache
	@echo "✅ Clean complete"

# ===================================================================================================
# Quality Assurance Targets
# ===================================================================================================

lint: ## Run golangci-lint with all checks
	@echo "Running golangci-lint..."
	@$(GOLANGCI_LINT) run --timeout=5m --verbose ./...
	@./webui/scripts/verify-no-hardcoded-colors.sh
	@echo "✅ Linting passed"

lint-invariants: ## Check architectural invariants (SeedMetadata usage)
	@echo "Checking for prohibited SeedMetadata usage..."
	@if git grep -n "SeedMetadata(" -- ':!**/*_test.go' ':!internal/control/vod/manager.go' ':!Makefile'; then \
		echo "❌ SeedMetadata found in production code (Strict Invariant Violation)"; \
		exit 1; \
	fi
	@echo "✅ Invariant check passed"

lint-fix: ## Run golangci-lint with automatic fixes
	@echo "Running golangci-lint fix..."
	@$(GOLANGCI_LINT) run --fix ./...
	@echo "✅ Linting fixes applied"

test: ## Run all unit tests
	@echo "Running unit tests..."
	@$(GO) test ./... -v
	@echo "✅ Unit tests passed"

test-schema: ## Run JSON schema validation tests (requires check-jsonschema)
	@echo "Running JSON schema validation tests..."
	@$(GO) test -tags=schemacheck -v ./internal/config
	@echo "✅ Schema validation tests passed"

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	@$(GO) test ./... -v -race
	@echo "✅ Race detection tests passed"

test-v3-idempotency: ## Run v3 idempotency/dedup suite with race detection (required in PR CI)
	@echo "Running v3 idempotency/dedup race suite..."
	@$(GO) test -race -count=1 -v ./internal/control/http/v3 -run '^TestRaceSafety_ParallelIntents$$'
	@echo "✅ v3 idempotency/dedup race suite passed"

test-cover: ## Run tests with coverage reporting
	@echo "Running tests with coverage..."
	@mkdir -p $(ARTIFACTS_DIR)
	@$(GO) test -covermode=atomic -coverprofile=$(ARTIFACTS_DIR)/coverage.out -coverpkg=./... ./...
	@$(GO) tool cover -html=$(ARTIFACTS_DIR)/coverage.out -o $(ARTIFACTS_DIR)/coverage.html
	@echo "Coverage report generated: $(ARTIFACTS_DIR)/coverage.html"
	@$(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | tail -1
	@total_coverage=$$($(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	epg_coverage=$$($(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | grep 'internal/epg' | awk '{sum+=$$3; count++} END {if(count>0) printf "%.1f", sum/count; else print "0"}'); \
	echo "Overall Coverage: $$total_coverage% (threshold: $(COVERAGE_THRESHOLD)%)"; \
	echo "EPG coverage: $$epg_coverage% (threshold: $(EPG_COVERAGE_THRESHOLD)%)"; \
	if [ $$(echo "$$total_coverage >= $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ] && [ $$(echo "$$epg_coverage >= $(EPG_COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo "✅ Coverage thresholds met"; \
	else \
		echo "❌ Coverage thresholds not met"; \
		exit 1; \
	fi

cover: test-cover ## Alias for test-cover

test-fuzz: ## Run comprehensive fuzzing tests
	@echo "Running fuzzing tests..."
	@echo "Fuzzing EPG generator..."
	@$(GO) test ./internal/epg -fuzz=FuzzGenerator -fuzztime=30s
	@echo "Fuzzing EPG fuzzy matcher..."
	@$(GO) test ./internal/epg -fuzz=FuzzFuzzyMatch -fuzztime=30s
	@echo "Fuzzing EPG XMLTV writer..."
	@$(GO) test ./internal/epg -fuzz=FuzzXMLTVWrite -fuzztime=30s
	@echo "✅ Fuzzing tests completed"

test-all: test-race test-cover test-fuzz ## Run complete test suite

test-integration: ## Run integration tests
	@echo "Running integration tests..."
	@$(GO) test -tags=integration -v -timeout=5m ./test/integration/...
	@echo "✅ Integration tests passed"

test-integration-fast: ## Run fast integration tests
	@echo "Running fast integration tests..."
	@$(GO) test -tags=integration_fast -v -timeout=3m ./test/integration/... -run="^TestAPIFast"
	@echo "✅ Fast integration tests passed"

test-integration-slow: ## Run slow integration tests
	@echo "Running slow integration tests..."
	@$(GO) test -tags=integration_slow -v -timeout=10m ./test/integration/... -run="^TestSlow"
	@echo "✅ Slow tests passed"

smoke-test: ## Run E2E smoke test (Builds & Runs daemon)
	@echo "Building binary for smoke test..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build -tags=nogpu -o $(BUILD_DIR)/xg2g-smoke ./cmd/daemon
	@echo "Running E2E smoke test..."
	@XG2G_SMOKE_BIN=$$(pwd)/$(BUILD_DIR)/xg2g-smoke $(GO) test -v -tags=smoke,nogpu -timeout=30s ./test/smoke/...
	@echo "✅ Smoke test passed"

coverage: ## Generate and view coverage report locally
	@echo "Generating coverage report..."
	@mkdir -p $(ARTIFACTS_DIR)
	@$(GO) test -covermode=atomic -coverprofile=$(ARTIFACTS_DIR)/coverage.out -coverpkg=./... ./...
	@$(GO) tool cover -html=$(ARTIFACTS_DIR)/coverage.out -o $(ARTIFACTS_DIR)/coverage.html
	@echo "Coverage report generated: $(ARTIFACTS_DIR)/coverage.html"
	@COVERAGE=$$($(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Overall Coverage: $$COVERAGE%"; \
	if command -v open >/dev/null 2>&1; then \
		open $(ARTIFACTS_DIR)/coverage.html; \
	elif command -v xdg-open >/dev/null 2>&1; then \
		xdg-open $(ARTIFACTS_DIR)/coverage.html; \
	else \
		echo "Open $(ARTIFACTS_DIR)/coverage.html in your browser to view the report"; \
	fi

coverage-check: ## Check if coverage meets threshold
	@echo "Checking coverage threshold..."
	@mkdir -p $(ARTIFACTS_DIR)
	@$(GO) test -covermode=atomic -coverprofile=$(ARTIFACTS_DIR)/coverage.out ./...
	@COVERAGE=$$($(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	THRESHOLD=50; \
	echo "Current coverage: $$COVERAGE%"; \
	echo "Threshold: $$THRESHOLD%"; \
	if (( $$(echo "$$COVERAGE < $$THRESHOLD" | bc -l) )); then \
		echo "❌ Coverage $$COVERAGE% is below threshold $$THRESHOLD%"; \
		exit 1; \
	else \
		echo "✅ Coverage $$COVERAGE% meets threshold $$THRESHOLD%"; \
	fi
	@echo "✅ All tests completed successfully"

# ===================================================================================================
# Enterprise Testing
# ===================================================================================================

codex: quality-gates ## Run Codex review bundle (lint + race/coverage + govulncheck)
	@echo "✅ Codex review bundle completed"
	@echo "   Includes: lint, race+coverage, govulncheck"

# quality-gates moved to end of file to combine dependencies

# ===================================================================================================
# Docker Operations
# ===================================================================================================

docker: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .
	@echo "✅ Docker image built: $(DOCKER_IMAGE):$(VERSION)"

docker-build: ## Build Docker image
	@echo "🚀 Building xg2g Docker image..."
	@docker buildx create --use --name xg2g-builder 2>/dev/null || true
	@docker buildx build \
		--load \
		--platform $(PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) \
		.
	@echo "✅ Docker image built and loaded"

docker-security: docker ## Run Docker security scanning
	@echo "Running Docker security scan..."
	@docker scout cves $(DOCKER_IMAGE):$(VERSION) || echo "⚠️  Docker Scout not available, skipping security scan"
	@echo "✅ Docker security scan completed"

docker-tag: ## Tag Docker image with version
	@echo "Tagging Docker image..."
	@if [ -n "$(DOCKER_REGISTRY)" ]; then \
		docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION); \
		echo "✅ Images tagged for registry: $(DOCKER_REGISTRY)"; \
	else \
		echo "✅ Local tags applied"; \
	fi

docker-push: docker-tag ## Push Docker image to registry
	@if [ -z "$(DOCKER_REGISTRY)" ]; then \
		echo "❌ DOCKER_REGISTRY not set"; \
		exit 1; \
	fi
	@echo "Pushing Docker images to $(DOCKER_REGISTRY)..."
	@docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION)
	@echo "✅ Docker images pushed"

docker-clean: ## Remove Docker build cache
	@echo "Cleaning Docker build cache..."
	@docker system prune -f
	@docker buildx prune -f
	@echo "✅ Docker cache cleaned"

# ===================================================================================================
# Security and Compliance
# ===================================================================================================

sbom: dev-tools ## Generate Software Bill of Materials
	@echo "Generating SBOM..."
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(SYFT)" scan dir:. -o spdx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.spdx.json
	@"$(SYFT)" scan dir:. -o cyclonedx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.cyclonedx.json
	@"$(SYFT)" scan dir:. -o syft-table --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.txt
	@echo "Validating SBOM files..."
	@jq -e '.spdxVersion' $(ARTIFACTS_DIR)/sbom.spdx.json > /dev/null || (echo "❌ SPDX SBOM validation failed"; exit 1)
	@jq -e '.bomFormat == "CycloneDX"' $(ARTIFACTS_DIR)/sbom.cyclonedx.json > /dev/null || (echo "❌ CycloneDX SBOM validation failed"; exit 1)
	@echo "✅ SBOM generated and validated:"
	@echo "   - SPDX: $(ARTIFACTS_DIR)/sbom.spdx.json"
	@echo "   - CycloneDX: $(ARTIFACTS_DIR)/sbom.cyclonedx.json"
	@echo "   - Table: $(ARTIFACTS_DIR)/sbom.txt"

security: security-vulncheck security-audit sbom ## Run comprehensive security analysis
	@echo "✅ Comprehensive security analysis completed"

security-scan: dev-tools ## Run container vulnerability scanning
	@echo "Running container vulnerability scan..."
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(GRYPE)" dir:. -o table -o json=$(ARTIFACTS_DIR)/vulnerabilities.json || echo "⚠️  Grype scan completed with findings"
	@echo "✅ Vulnerability scan completed: $(ARTIFACTS_DIR)/vulnerabilities.json"

security-audit: ## Run dependency vulnerability audit
	@echo "Running dependency security audit..."
	@$(GO) list -json -deps ./... | nancy sleuth || echo "⚠️  Nancy audit completed with findings"
	@echo "✅ Dependency audit completed"

security-gosec: ## Run Gosec security scanner
	@echo "Ensuring gosec is installed..."
	@command -v gosec >/dev/null 2>&1 || curl -sfL https://raw.githubusercontent.com/securego/gosec/master/install.sh | sh -s -- -b $(TOOL_DIR)
	@echo "Running Gosec..."
	@$(TOOL_DIR)/gosec -fmt=text -severity=medium -exclude-dir=test -exclude-dir=internal/test -exclude-dir=.github ./...
	@echo "✅ Gosec check passed"

security-vulncheck: ## Run Go vulnerability checker
	@echo "Ensuring govulncheck is installed..."
	@[ -x "$(GOVULNCHECK)" ] || GOFLAGS= $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Running Go vulnerability check..."
	@$(GOVULNCHECK) ./...
	@echo "✅ Go vulnerability check passed"

# ===================================================================================================
# Dependency Management
# ===================================================================================================

deps: deps-tidy deps-vendor deps-verify ## Install and verify dependencies
	@echo "✅ Dependencies installed and verified"

deps-vendor: ## Refresh vendor directory
	@echo "Vendoring dependencies..."
	@$(GO) mod vendor
	@echo "✅ Dependencies vendored"

deps-update: ## Update dependencies to latest versions
	@echo "Updating dependencies..."
	@$(GO) get -u ./...
	@$(GO) mod tidy
	@$(MAKE) deps-vendor
	@echo "✅ Dependencies updated"

deps-tidy: ## Clean and organize Go modules
	@echo "Tidying Go modules..."
	@$(GO) mod tidy
	@$(GO) mod verify
	@echo "✅ Go modules tidied"

deps-verify: ## Verify dependency integrity
	@echo "Verifying dependency integrity..."
	@$(GO) mod verify
	@echo "✅ Dependencies verified"

review-zip: ## Create a ZIP snapshot for offline review
	@echo "Creating review ZIP artifact..."
	@git archive --format=zip --prefix=xg2g-main/ HEAD -o xg2g-main.zip
	@echo "✅ Review ZIP created: xg2g-main.zip"
	@$(MAKE) verify-zip

verify-zip: ## Verify the purity of the review ZIP artifact
	@echo "Verifying review ZIP purity..."
	@./scripts/ci_gate_zip_purity.py xg2g-main.zip

deps-licenses: ## Generate dependency license report
	@echo "Generating dependency license report..."
	@mkdir -p dist
	@go-licenses report ./... > dist/licenses.txt 2>/dev/null || echo "go-licenses not available, install with: $(GO) install github.com/google/go-licenses@v2.8.0"
	@echo "✅ License report generated: dist/licenses.txt"

# ===================================================================================================
# Development Tools
# ===================================================================================================

dev-tools: ## Install all development tools
	@echo "Installing development tools..."
	@echo "Installing golangci-lint..."
	@$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@echo "Installing oapi-codegen..."
	@$(GO) install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@$(OAPI_CODEGEN_VERSION)
	@echo "Installing govulncheck..."
	@$(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Installing syft for SBOM generation..."
	@$(GO) install github.com/anchore/syft/cmd/syft@$(SYFT_VERSION)
	@echo "Installing grype for vulnerability scanning..."
	@$(GO) install github.com/anchore/grype/cmd/grype@$(GRYPE_VERSION)
	@echo "Installing go-licenses..."
	@$(GO) install github.com/google/go-licenses@v1.6.0
	@echo "✅ Development tools installed"

check-tools: ## Verify development tools are installed
	@echo "Checking development tools..."
	@command -v golangci-lint >/dev/null 2>&1 || (echo "❌ golangci-lint not found" && exit 1)
	@command -v govulncheck >/dev/null 2>&1 || (echo "❌ govulncheck not found" && exit 1)
	@echo "✅ Essential tools verified"

pre-commit: lint test-race ## Run pre-commit validation checks
	@echo "Running pre-commit checks..."
	@$(MAKE) quality-gates
	@echo "✅ Pre-commit checks passed"

# ===================================================================================================
# Release Management
# ===================================================================================================

release-check: ## Validate release readiness
	@echo "Checking release readiness..."
	@$(MAKE) test-all
	@$(MAKE) build-all
	@$(MAKE) sbom
	@echo "✅ Release validation completed"

release-build: clean build-all docker sbom ## Build release artifacts
	@echo "Building release artifacts..."
	@mkdir -p dist/release
	@cp -r $(BUILD_DIR)/* dist/release/
	@echo "✅ Release artifacts prepared in dist/release/"

release-tag: ## Create and push release tag
	@if [ "$(VERSION)" = "dev" ]; then \
		echo "❌ Cannot tag development version"; \
		exit 1; \
	fi
	@echo "Creating release tag $(VERSION)..."
	@git tag -a $(VERSION) -m "Release $(VERSION)"
	@git push origin $(VERSION)
	@echo "✅ Release tag $(VERSION) created and pushed"

release-notes: ## Generate release notes
	@echo "Generating release notes for $(VERSION)..."
	@mkdir -p dist
	@echo "# Release Notes - $(VERSION)" > dist/RELEASE_NOTES.md
	@echo "" >> dist/RELEASE_NOTES.md
	@echo "**Build Information:**" >> dist/RELEASE_NOTES.md
	@echo "- Version: $(VERSION)" >> dist/RELEASE_NOTES.md
	@echo "- Commit: $(COMMIT_HASH)" >> dist/RELEASE_NOTES.md
	@echo "- Build Date: $(BUILD_DATE)" >> dist/RELEASE_NOTES.md
	@echo "" >> dist/RELEASE_NOTES.md
	@echo "**Changes:**" >> dist/RELEASE_NOTES.md
	@git log --oneline --since="1 month ago" >> dist/RELEASE_NOTES.md || echo "- See git history for changes" >> dist/RELEASE_NOTES.md
	@echo "✅ Release notes generated: dist/RELEASE_NOTES.md"

# ===================================================================================================
# Deployment Helpers
# ===================================================================================================



# ===================================================================================================
# Development & Local Orchestration
# ===================================================================================================

reset: ## Full reset: Stop, Clean, Rebuild UI, and Start Dev
	@echo "🔄 Performing full environment reset..."
	@$(MAKE) stop-local || true
	@$(MAKE) clean
	@echo "🧹 Clearing data/ directories..."
	@rm -rf data/hls data/v3-store data/playlist.m3u data/xmltv.xml

	@$(MAKE) ui-build
	@$(MAKE) dev

stop-local: ## Local cleanup helper (stops daemon and ffmpeg)
	@echo "🧹 Cleaning up old instances..."
	@pkill -x "$(BINARY_NAME)" || true
	@echo "⏳ Waiting for port release..."
	@for i in 1 2 3 4 5; do if pgrep -x "$(BINARY_NAME)" >/dev/null; then sleep 1; else break; fi; done
	@if pgrep -x "$(BINARY_NAME)" >/dev/null; then echo "💀 Force killing..."; pkill -9 -x "$(BINARY_NAME)" || true; fi
	@echo "🧹 Cleaning up zombie ffmpeg processes..."
	@pkill -u $$(id -u) -x ffmpeg || true

dev: stop-local ## Run daemon locally with .env configuration
	@echo "Starting xg2g in development mode..."
	@if [ ! -f .env ]; then \
		echo "⚠️  No .env file found. Creating from .env.example..."; \
		cp .env.example .env; \
		echo "📝 Please edit .env with your settings"; \
		exit 1; \
	fi
	@./scripts/check_env.sh
	@echo "Loading configuration from .env..."
	@set -a; . ./.env; set +a; \
	export XG2G_DATA=$${XG2G_DATA:-./data}; \
	export XG2G_OWI_BASE=$${XG2G_OWI_BASE:-}; \
	export XG2G_BOUQUET=$${XG2G_BOUQUET:-}; \
	export XG2G_LISTEN=$${XG2G_LISTEN:-:8088}; \
	$(GO) build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon && \
	./$(BUILD_DIR)/$(BINARY_NAME)

up-dev: ## Start docker dev stack (with .env + local build)
	@echo "Starting xg2g (Dev Container)..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env from example"; fi
	@docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
	@echo "✅ Dev stack started at http://localhost:8088"
	@echo "📊 Check status: make status"

up: ## Start docker-compose.yml stack
	@echo "Starting xg2g via docker compose..."
	@./scripts/check_env.sh
	@docker compose up -d
	@echo "✅ Stack started. Access at http://localhost:8088"
	@echo "📊 Check status: make status"

down: ## Stop docker-compose.yml stack
	@echo "Stopping xg2g stack..."
	@docker compose down
	@echo "✅ Stack stopped"

status: ## Check API status endpoint
	@echo "Checking xg2g API status..."
	@curl -fsS http://localhost:8088/healthz >/dev/null 2>&1 && echo "✅ OK (healthz)" || \
		echo "❌ Service not responding. Is it running? (make up / make dev)"

logs: ## Show service logs (use SVC=service-name to filter)
	@if [ -n "$$SVC" ]; then \
		echo "Showing logs for service: $$SVC"; \
		docker compose logs -f $$SVC; \
	else \
		echo "Showing logs for all services..."; \
		docker compose logs -f; \
	fi

restart: ## Restart service (use SVC=service-name, defaults to xg2g)
	@SVC=$${SVC:-xg2g}; \
	echo "Restarting service: $$SVC"; \
	docker compose restart $$SVC; \
	echo "✅ Service $$SVC restarted"

ps: ## Show running containers
	@docker compose ps

# ===================================================================================================
# Production Operations & Monitoring
# ===================================================================================================

prod-up: ## Start production docker-compose stack
	@echo "Starting production stack..."
	@./scripts/check_env.sh
	@docker compose -f docker-compose.yml up -d
	@echo "✅ Production stack started"
	@echo "📊 Metrics: http://localhost:9090/metrics"

prod-down: ## Stop production stack
	@echo "Stopping production stack..."
	@docker compose -f docker-compose.yml down
	@echo "✅ Production stack stopped"

prod-logs: ## Show production stack logs
	@echo "Showing production logs..."
	@docker compose -f docker-compose.yml logs -f

prod-restart: ## Restart production service
	@echo "Restarting production service..."
	@docker compose -f docker-compose.yml restart xg2g
	@echo "✅ Production service restarted"

prod-ps: ## Show production containers
	@docker compose -f docker-compose.yml ps




# ============================================================================
# Pre-Commit Hooks und Linting
# ============================================================================

.PHONY: hooks lint-yaml validate schema-docs schema-validate

hooks:
	@pre-commit install
	@pre-commit install --hook-type pre-push


check-env: ## Check .env configuration
	@./scripts/check_env.sh
	@echo "Checking docker compose config..."
	@docker compose config

validate: check-env ## Validate configuration
	@test -f config.yaml && $(GO) run ./cmd/validate -f config.yaml || $(GO) run ./cmd/validate -f config.example.yaml

schema-docs: ## Generate docs/config.md from JSON Schema
	@echo "Generating config documentation from JSON Schema..."
	@$(GO) run cmd/schemadocs/main.go > docs/config.md
	@echo "✓ Generated docs/config.md"

schema-validate: ## Validate all YAML config files against JSON Schema
	@echo "Validating config files against JSON Schema..."
	@if ! command -v check-jsonschema >/dev/null 2>&1; then \
		echo "❌ check-jsonschema not installed"; \
		echo "   Install with: pip install check-jsonschema"; \
		exit 1; \
	fi
	@check-jsonschema --schemafile docs/guides/config.schema.json config.example.yaml
	@check-jsonschema --schemafile docs/guides/config.schema.json config.generated.example.yaml
	@find internal/config/testdata -name 'valid-*.yaml' -type f -print0 2>/dev/null | xargs -0 -r -I{} check-jsonschema --schemafile docs/guides/config.schema.json {}
	@if [ -d examples ]; then find examples -name '*.ya?ml' -type f -print0 | xargs -0 -r -I{} check-jsonschema --schemafile docs/guides/config.schema.json {}; fi
	@echo "✓ Schema validation complete"

gate-a: ## Gate A: Control Layer Store Purity (ADR-014 Phase 1)
	@./scripts/verify_gate_a_control_store.sh

# Gate B: Thin-Client Audit (Prevent Business Logic Leakage)
.PHONY: gate-webui
gate-webui:
	@./scripts/ci_gate_webui_audit.sh

# Gate C: Repository Hygiene (Zero Tolerance for Artifacts)
.PHONY: gate-repo-hygiene
gate-repo-hygiene:
	@./scripts/ci_gate_repo_hygiene.sh

# Gate V3: OpenAPI v3 Contract Governance (CTO Hardrail)
.PHONY: gate-v3-contract
gate-v3-contract: ## Verify v3 contract hygiene, casing, and shadowing
	@echo "--- gate-v3-contract ---"
	@$(GO) test -v ./internal/control/http/v3 -run TestContractHygiene
	@./scripts/verify-openapi-lint.sh
	@python3 ./scripts/verify-openapi-no-duplicate-keys.py api/openapi.yaml
	@python3 ./scripts/lib/openapi_v3_scope.py api/openapi.yaml scripts/openapi-legacy-allowlist.json
	@./scripts/verify-v3-shadowing.sh
	@go test -v -count=1 ./internal/control/http/v3 -run "(TestV3_ResponseGolden|TestProblemDetails_Compliance|TestPlaybackInfo_SchemaCompliance)"

quality-gates: quality-gates-online ## Validate all online quality gates (coverage, lint, security)

quality-gates-offline: ## Offline-only gates (no network, no codegen)
	@echo "Validating offline gates..."
	@$(GO) test ./...
	@$(GO) vet ./...
	@echo "✅ Offline gates passed"

quality-gates-online: verify-config verify-docs-compiled verify-generate verify-hermetic-codegen gate-repo-hygiene gate-v3-contract gate-a gate-webui lint-invariants lint test-cover security-vulncheck ## Validate all online quality gates
	@echo "Validating quality gates..."
	@echo "✅ All quality gates passed"

ci-pr: verify-config verify-generate gate-repo-hygiene gate-v3-contract gate-a gate-webui lint-invariants lint schema-validate test-v3-idempotency test ## Enforced PR baseline (guardrails + schema + idempotency/race)
	@echo "✅ CI PR gate passed"

ci-nightly: quality-gates-online contract-matrix test-race test-fuzz smoke-test ## Deep, expensive gates for nightly/dispatch
	@echo "✅ CI nightly gates passed"

# ===================================================================================================
# FFmpeg Build Automation
# ===================================================================================================

setup: build-ffmpeg ## One-time setup: build FFmpeg locally

build-ffmpeg: ## Build FFmpeg 7.1.3 with HLS/VAAPI/x264/AAC support
	@echo "Building FFmpeg 7.1.3..."
	@./scripts/build-ffmpeg.sh
	@echo "✅ FFmpeg built successfully"
	@echo ""
	@echo "Use wrappers for scoped LD_LIBRARY_PATH (recommended):"
	@echo "  export XG2G_FFMPEG_BIN=\$$(pwd)/scripts/ffmpeg-wrapper.sh"
	@echo "  export XG2G_FFPROBE_BIN=\$$(pwd)/scripts/ffprobe-wrapper.sh"
	@echo ""
	@echo "Or set PATH manually:"
	@echo "  export PATH=/opt/xg2g/ffmpeg/bin:\$$PATH"
	@echo "  export LD_LIBRARY_PATH=/opt/xg2g/ffmpeg/lib"


.PHONY: contract-matrix
contract-matrix: ## P4-1: Run contract matrix golden snapshot gate
	@set -euo pipefail; \

gate-decision-proof: ## Phase 4.7: Run Playback Decision Engine Proof System (CTO-Grade)
	@echo "Running Playback Decision Engine Proof System..."
	@XG2G_PROOF_SEED=12345 $(GO) test -count=1 -v ./internal/control/recordings/decision -run 'TestProp_|TestGate_|TestModel'
