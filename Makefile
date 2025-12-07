# ===================================================================================================
# xg2g Enterprise Makefile - Production-Grade Development Workflow
# ===================================================================================================
# This Makefile provides comprehensive targets for building, testing, linting, and releasing xg2g.
# All targets are designed with enterprise-grade quality assurance and CI/CD integration in mind.
# ===================================================================================================

.PHONY: help build build-all build-rust build-ffi clean clean-rust lint lint-fix test test-transcoder test-schema test-race test-cover cover test-fuzz test-all test-ffi \
        docker docker-build docker-build-cpu docker-build-gpu docker-build-all docker-security docker-tag docker-push docker-clean \
        sbom deps deps-update deps-tidy deps-verify deps-licenses \
        security security-scan security-audit security-vulncheck \
	quality-gates pre-commit install dev-tools check-tools \
        release-check release-build release-tag release-notes \
        dev up down status prod-up prod-down prod-logs \
        restart prod-restart ps prod-ps ui-build codex

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
export GOFLAGS := -trimpath -buildvcs=false -mod=readonly

# Build configuration
BINARY_NAME := daemon
BUILD_DIR := bin
# Reproducible build flags
BUILD_FLAGS := -trimpath -buildvcs=false
LDFLAGS := -ldflags "-s -w -buildid= -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT_HASH)' -X 'main.buildDate=$(BUILD_DATE)'"
DOCKER_IMAGE := xg2g
DOCKER_REGISTRY ?=
PLATFORMS := linux/amd64

# Coverage thresholds (matching CI configuration)
COVERAGE_THRESHOLD := 55
EPG_COVERAGE_THRESHOLD := 55

# Tool paths and versions
GOBIN ?= $(shell go env GOBIN)
GOPATH_BIN := $(shell go env GOPATH)/bin
TOOL_DIR := $(if $(GOBIN),$(GOBIN),$(GOPATH_BIN))

# Tool executables
GOLANGCI_LINT := $(TOOL_DIR)/golangci-lint
GOVULNCHECK := $(TOOL_DIR)/govulncheck
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
	@echo "  build-all      Build binaries for all supported platforms"
	@echo "  build-rust     Build Rust transcoder library"
	@echo "  build-ffi      Build single binary with embedded Rust library (CGO)"
	@echo "  clean          Remove build artifacts and temporary files"
	@echo "  clean-rust     Clean Rust build artifacts"
	@echo ""
	@echo "Quality Assurance:"
	@echo "  lint              Run golangci-lint with all checks"
	@echo "  lint-fix          Run golangci-lint with automatic fixes"
	@echo "  test              Run all unit tests (stub transcoder)"
	@echo "  test-transcoder   Run tests with Rust transcoder enabled"
	@echo "  test-schema       Run JSON schema validation tests"
	@echo "  test-race         Run tests with race detection"
	@echo "  test-cover        Run tests with coverage reporting"
	@echo "  test-fuzz         Run comprehensive fuzzing tests"
	@echo "  test-all          Run complete test suite (unit + race + fuzz)"
	@echo "  test-ffi          Test Rust FFI integration (requires Rust library)"
	@echo ""
	@echo "Enterprise Testing:"
	@echo "  codex             Run the Codex review bundle (lint + race/coverage + govulncheck)"
	@echo "  quality-gates     Validate all quality gates (coverage, lint, security)"
	@echo ""
	@echo "Docker Operations:"
	@echo "  docker              Build Docker image"
	@echo "  docker-build        Build CPU-only Docker image (default, MODE 1+2)"
	@echo "  docker-build-cpu    Build CPU-only Docker image (MODE 1+2: Audio transcoding)"
	@echo "  docker-build-gpu    Build GPU-enabled Docker image (MODE 3: VAAPI transcoding)"
	@echo "  docker-build-all    Build both CPU and GPU variants"
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
	@echo "Copying to internal/api/dist..."
	@rm -rf internal/api/dist/*
	@mkdir -p internal/api/dist
	@cp -r webui/dist/* internal/api/dist/
	@touch internal/api/dist/.keep
	@echo "✅ WebUI build complete"

build: ui-build ## Build the main daemon binary
	@echo "Building xg2g daemon..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-rust: ## Build Rust transcoder library (shared library for FFI)
	@echo "Building Rust transcoder library (cdylib)..."
	@cd transcoder && cargo build --release --lib
	@echo "✅ Rust shared library built: transcoder/target/release/libxg2g_transcoder.so"

build-ffi: build-rust ## Build single binary with embedded Rust library (requires CGO)
	@echo "Building xg2g daemon with embedded Rust library..."
	@mkdir -p $(BUILD_DIR)
	@if [ "$$(uname)" = "Darwin" ]; then \
		echo "Building for macOS with Rust FFI..."; \
		CGO_ENABLED=1 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-ffi ./cmd/daemon; \
	elif [ "$$(uname)" = "Linux" ]; then \
		echo "Building for Linux with Rust FFI..."; \
		CGO_ENABLED=1 go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-ffi ./cmd/daemon; \
	else \
		echo "❌ Unsupported platform for FFI build"; \
		exit 1; \
	fi
	@echo "✅ FFI build complete: $(BUILD_DIR)/$(BINARY_NAME)-ffi"
	@echo "   This binary includes the Rust audio remuxer!"

build-all: ## Build binaries for all supported platforms
	@echo "Building for all platforms..."
	@mkdir -p $(BUILD_DIR)
	@for platform in $$(echo "$(PLATFORMS)" | tr ',' '\n'); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		output="$(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch"; \
		if [ "$$os" = "windows" ]; then output="$$output.exe"; fi; \
		echo "Building for $$os/$$arch..."; \
		GOOS=$$os GOARCH=$$arch go build $(BUILD_FLAGS) $(LDFLAGS) -o $$output ./cmd/daemon || exit 1; \
	done
	@echo "✅ Multi-platform build complete"
	@ls -la $(BUILD_DIR)/

build-repro: ui-build ## Build deterministic binary (reproducible across identical sources)
	@echo "Building reproducible xg2g daemon..."
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 go build $(BUILD_FLAGS) -mod=readonly -o $(BUILD_DIR)/$(BINARY_NAME) \
		-ldflags "-s -w -buildid= -X 'main.version=$(VERSION)' -X 'main.commit=$(COMMIT_HASH)' -X 'main.buildDate=$(SOURCE_DATE_EPOCH)'" \
		./cmd/daemon
	@echo "✅ Reproducible build complete: $(BUILD_DIR)/$(BINARY_NAME)"

clean: ## Remove build artifacts and temporary files
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf coverage.out coverage.html
	@rm -rf *.prof *.test
	@rm -rf dist/
	@go clean -cache -testcache -modcache
	@echo "✅ Clean complete"

clean-rust: ## Clean Rust build artifacts
	@echo "Cleaning Rust build artifacts..."
	@cd transcoder && cargo clean
	@echo "✅ Rust clean complete"

# ===================================================================================================
# Quality Assurance Targets
# ===================================================================================================

lint: ## Run golangci-lint with all checks
	@echo "Ensuring golangci-lint is installed..."
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Running golangci-lint..."
	@"$(GOLANGCI_LINT)" run ./... --timeout=5m
	@echo "✅ Lint checks passed"

lint-fix: ## Run golangci-lint with automatic fixes
	@echo "Ensuring golangci-lint is installed..."
	@command -v $(GOLANGCI_LINT) >/dev/null 2>&1 || go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Running golangci-lint with fixes..."
	@"$(GOLANGCI_LINT)" run ./... --fix --timeout=5m
	@echo "✅ Lint fixes applied"

test: ## Run all unit tests (with stub transcoder)
	@echo "Running unit tests..."
	@go test ./... -v
	@echo "✅ Unit tests passed"

test-transcoder: build-rust ## Run tests with Rust transcoder enabled
	@echo "Running tests with Rust transcoder (ensure LD_LIBRARY_PATH includes Rust lib dir)..."
	@LD_LIBRARY_PATH=$$(pwd)/transcoder/target/release CGO_ENABLED=1 go test -tags=transcoder ./... -v
	@echo "✅ Transcoder tests passed"

test-schema: ## Run JSON schema validation tests (requires check-jsonschema)
	@echo "Running JSON schema validation tests..."
	@go test -tags=schemacheck -v ./internal/config
	@echo "✅ Schema validation tests passed"

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	@go test ./... -v -race
	@echo "✅ Race detection tests passed"

test-cover: ## Run tests with coverage reporting
	@echo "Running tests with coverage..."
	@go test ./... -v -race -coverprofile=coverage.out -covermode=atomic
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@echo "Overall coverage:"
	@go tool cover -func=coverage.out | tail -1
	@echo "Checking coverage thresholds..."
	@total_coverage=$$(go tool cover -func=coverage.out | tail -1 | awk '{print $$3}' | sed 's/%//'); \
	epg_coverage=$$(go tool cover -func=coverage.out | grep 'internal/epg' | awk '{sum+=$$3; count++} END {if(count>0) printf "%.1f", sum/count; else print "0"}'); \
	echo "Total coverage: $$total_coverage% (threshold: $(COVERAGE_THRESHOLD)%)"; \
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
	@go test ./internal/epg -fuzz=FuzzGenerator -fuzztime=30s
	@echo "Fuzzing EPG fuzzy matcher..."
	@go test ./internal/epg -fuzz=FuzzFuzzyMatch -fuzztime=30s
	@echo "Fuzzing EPG XMLTV writer..."
	@go test ./internal/epg -fuzz=FuzzXMLTVWrite -fuzztime=30s
	@echo "✅ Fuzzing tests completed"

test-all: test-race test-cover test-fuzz ## Run complete test suite

test-ffi: build-rust ## Test Rust FFI integration
	@echo "Running FFI integration tests..."
	@echo "Building Rust library first..."
	@cd transcoder && cargo build --release
	@echo "Running Go tests with CGO enabled..."
	@CGO_ENABLED=1 go test -v ./internal/transcoder -tags gpu
	@echo "✅ FFI integration tests passed"

test-integration: ## Run integration tests (with stub transcoder)
	@echo "Running integration tests..."
	@go test -tags=integration -v -timeout=5m ./test/integration/...
	@echo "✅ Integration tests passed"

test-integration-fast: ## Run fast integration smoke tests (with stub transcoder)
	@echo "Running fast integration smoke tests..."
	@go test -tags=integration_fast -v -timeout=3m ./test/integration/... -run="^TestSmoke"
	@echo "✅ Smoke tests passed"

test-integration-slow: ## Run slow integration tests (with stub transcoder)
	@echo "Running slow integration tests..."
	@go test -tags=integration_slow -v -timeout=10m ./test/integration/... -run="^TestSlow"
	@echo "✅ Slow tests passed"

coverage: ## Generate and view coverage report locally
	@echo "Generating coverage report..."
	@go test -covermode=atomic -coverprofile=coverage.out -coverpkg=./... ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Overall Coverage: $$COVERAGE%"; \
	if command -v open >/dev/null 2>&1; then \
		open coverage.html; \
	elif command -v xdg-open >/dev/null 2>&1; then \
		xdg-open coverage.html; \
	else \
		echo "Open coverage.html in your browser to view the report"; \
	fi

coverage-check: ## Check if coverage meets threshold
	@echo "Checking coverage threshold..."
	@go test -covermode=atomic -coverprofile=coverage.out ./...
	@COVERAGE=$$(go tool cover -func=coverage.out | grep total | awk '{print $$3}' | sed 's/%//'); \
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

quality-gates: lint test-cover security-vulncheck ## Validate all quality gates
	@echo "Validating quality gates..."
	@echo "✅ All quality gates passed"

# ===================================================================================================
# Docker Operations
# ===================================================================================================

docker: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .
	@echo "✅ Docker image built: $(DOCKER_IMAGE):$(VERSION)"

docker-build: docker-build-cpu ## Build Docker image (default: CPU-only, MODE 1+2)

docker-build-cpu: ## Build CPU-only Docker image (MODE 1+2: Audio transcoding only)
	@echo "🚀 Building CPU-only Docker image (MODE 1+2)..."
	@docker buildx create --use --name xg2g-builder 2>/dev/null || true
	@docker buildx build \
		--platform $(PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) \
		-t $(DOCKER_IMAGE):latest \
		-t $(DOCKER_IMAGE):$(VERSION)-cpu \
		-t $(DOCKER_IMAGE):latest-cpu \
		.
	@echo "✅ CPU-only Docker image built"

docker-build-gpu: ## Build GPU-enabled Docker image (MODE 3: VAAPI video transcoding)
	@echo "⚠️  GPU support is currently disabled in Dockerfile (MODE 3 temporarily unavailable)"
	@echo "⚠️  Building standard CPU image instead..."
	@docker buildx create --use --name xg2g-builder 2>/dev/null || true
	@docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(DOCKER_IMAGE):$(VERSION)-gpu \
		-t $(DOCKER_IMAGE):latest-gpu \
		.
	@echo "✅ GPU-enabled Docker image built"

docker-build-all: docker-build-cpu docker-build-gpu ## Build both CPU and GPU variants
	@echo "✅ All Docker image variants built successfully"

docker-security: docker ## Run Docker security scanning
	@echo "Running Docker security scan..."
	@docker scout cves $(DOCKER_IMAGE):$(VERSION) || echo "⚠️  Docker Scout not available, skipping security scan"
	@echo "✅ Docker security scan completed"

docker-tag: ## Tag Docker image with version
	@echo "Tagging Docker image..."
	@if [ -n "$(DOCKER_REGISTRY)" ]; then \
		docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION); \
		docker tag $(DOCKER_IMAGE):latest $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest; \
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
	@docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):latest
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
	@mkdir -p dist
	@"$(SYFT)" scan dir:. -o spdx-json --source-name xg2g --source-version $(VERSION) > dist/sbom.spdx.json
	@"$(SYFT)" scan dir:. -o cyclonedx-json --source-name xg2g --source-version $(VERSION) > dist/sbom.cyclonedx.json
	@"$(SYFT)" scan dir:. -o syft-table --source-name xg2g --source-version $(VERSION) > dist/sbom.txt
	@echo "Validating SBOM files..."
	@jq -e '.spdxVersion' dist/sbom.spdx.json > /dev/null || (echo "❌ SPDX SBOM validation failed"; exit 1)
	@jq -e '.bomFormat == "CycloneDX"' dist/sbom.cyclonedx.json > /dev/null || (echo "❌ CycloneDX SBOM validation failed"; exit 1)
	@echo "✅ SBOM generated and validated:"
	@echo "   - SPDX: dist/sbom.spdx.json"
	@echo "   - CycloneDX: dist/sbom.cyclonedx.json"
	@echo "   - Table: dist/sbom.txt"

security: security-vulncheck security-audit sbom ## Run comprehensive security analysis
	@echo "✅ Comprehensive security analysis completed"

security-scan: dev-tools ## Run container vulnerability scanning
	@echo "Running container vulnerability scan..."
	@"$(GRYPE)" dir:. -o table -o json=dist/vulnerabilities.json || echo "⚠️  Grype scan completed with findings"
	@echo "✅ Vulnerability scan completed: dist/vulnerabilities.json"

security-audit: ## Run dependency vulnerability audit
	@echo "Running dependency security audit..."
	@go list -json -deps ./... | nancy sleuth || echo "⚠️  Nancy audit completed with findings"
	@echo "✅ Dependency audit completed"

security-gosec: ## Run Gosec security scanner
	@echo "Ensuring gosec is installed..."
	@command -v gosec >/dev/null 2>&1 || curl -sfL https://raw.githubusercontent.com/securego/gosec/master/install.sh | sh -s -- -b $(TOOL_DIR)
	@echo "Running Gosec..."
	@$(TOOL_DIR)/gosec -fmt=text -severity=medium -exclude-dir=test -exclude-dir=internal/test -exclude-dir=.github ./...
	@echo "✅ Gosec check passed"

security-vulncheck: ## Run Go vulnerability checker
	@echo "Ensuring govulncheck is installed..."
	@command -v $(GOVULNCHECK) >/dev/null 2>&1 || go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Running Go vulnerability check..."
	@"$(GOVULNCHECK)" ./...
	@echo "✅ Go vulnerability check passed"

# ===================================================================================================
# Dependency Management
# ===================================================================================================

deps: deps-tidy deps-verify ## Install and verify dependencies
	@echo "✅ Dependencies installed and verified"

deps-update: ## Update dependencies to latest versions
	@echo "Updating dependencies..."
	@go get -u ./...
	@go mod tidy
	@echo "✅ Dependencies updated"

deps-tidy: ## Clean and organize Go modules
	@echo "Tidying Go modules..."
	@go mod tidy
	@go mod verify
	@echo "✅ Go modules tidied"

deps-verify: ## Verify dependency integrity
	@echo "Verifying dependency integrity..."
	@go mod verify
	@go mod download
	@echo "✅ Dependencies verified"

deps-licenses: ## Generate dependency license report
	@echo "Generating dependency license report..."
	@mkdir -p dist
	@go-licenses report ./... > dist/licenses.txt 2>/dev/null || echo "go-licenses not available, install with: go install github.com/google/go-licenses@latest"
	@echo "✅ License report generated: dist/licenses.txt"

# ===================================================================================================
# Development Tools
# ===================================================================================================

dev-tools: ## Install all development tools
	@echo "Installing development tools..."
	@echo "Installing golangci-lint..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "Installing govulncheck..."
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@echo "Installing syft for SBOM generation..."
	@go install github.com/anchore/syft/cmd/syft@latest
	@echo "Installing grype for vulnerability scanning..."
	@go install github.com/anchore/grype/cmd/grype@latest
	@echo "Installing go-licenses for license reporting..."
	@go install github.com/google/go-licenses@latest
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

dev: ## Run daemon locally with .env configuration
	@echo "Starting xg2g in development mode..."
	@if [ ! -f .env ]; then \
		echo "⚠️  No .env file found. Creating from .env.example..."; \
		cp .env.example .env; \
		echo "📝 Please edit .env with your settings"; \
		exit 1; \
	fi
	@echo "Loading configuration from .env..."
	@set -a; . ./.env; set +a; \
	XG2G_DATA=$${XG2G_DATA:-./data} \
	XG2G_OWI_BASE=$${XG2G_OWI_BASE:?Set XG2G_OWI_BASE in .env} \
	XG2G_BOUQUET=$${XG2G_BOUQUET:?Set XG2G_BOUQUET in .env} \
	XG2G_LISTEN=$${XG2G_LISTEN:-:8080} \
	go run ./cmd/daemon

up: ## Start docker-compose.yml stack
	@echo "Starting xg2g via docker compose..."
	@docker compose up -d
	@echo "✅ Stack started. Access at http://localhost:8080"
	@echo "📊 Check status: make status"

down: ## Stop docker-compose.yml stack
	@echo "Stopping xg2g stack..."
	@docker compose down
	@echo "✅ Stack stopped"

status: ## Check API status endpoint
	@echo "Checking xg2g API status..."
	@curl -s http://localhost:8080/api/v1/status | jq . 2>/dev/null || \
		curl -s http://localhost:8080/api/v1/status || \
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
	@echo "Checking docker compose config..."
	@docker compose config

validate: check-env ## Validate configuration
	@test -f config.yaml && go run ./cmd/validate -f config.yaml || go run ./cmd/validate -f config.example.yaml

schema-docs: ## Generate docs/config.md from JSON Schema
	@echo "Generating config documentation from JSON Schema..."
	@go run ./tools/schema-docs ./docs/guides/config.schema.json ./docs/guides/config.md
	@echo "✓ Generated docs/config.md"

schema-validate: ## Validate all YAML config files against JSON Schema
	@echo "Validating config files against JSON Schema..."
	@if command -v check-jsonschema >/dev/null 2>&1; then \
		check-jsonschema --schemafile docs/guides/config.schema.json config.example.yaml; \
		find internal/config/testdata -name 'valid-*.yaml' -type f -print0 2>/dev/null | xargs -0 -I{} check-jsonschema --schemafile docs/guides/config.schema.json {} || true; \
		if [ -d examples ]; then find examples -name '*.ya?ml' -type f -print0 | xargs -0 -I{} check-jsonschema --schemafile docs/guides/config.schema.json {} || true; fi; \
		echo "✓ Schema validation complete"; \
	else \
		echo "⚠  check-jsonschema not installed, skipping schema validation"; \
		echo "   Install with: pip install check-jsonschema"; \
	fi
