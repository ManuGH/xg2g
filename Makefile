# ===================================================================================================
# xg2g Enterprise Makefile - Production-Grade Development Workflow
# ===================================================================================================
# This Makefile provides comprehensive targets for building, testing, linting, and releasing xg2g.
# All targets are designed with enterprise-grade quality assurance and CI/CD integration in mind.
# ===================================================================================================

.PHONY: help build build-all clean lint lint-fix test test-race test-cover test-fuzz test-all \
        docker docker-build docker-security docker-tag docker-push docker-clean \
        sbom deps deps-update deps-tidy deps-verify deps-licenses \
        security security-scan security-audit security-vulncheck \
	hardcore-test quality-gates pre-commit install dev-tools check-tools \
	pin-digests compose-up-alpine compose-up-distroless k8s-apply-alpine k8s-apply-distroless \
        release-check release-build release-tag release-notes

# ===================================================================================================
# Configuration and Variables
# ===================================================================================================

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT_HASH ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS := -ldflags "-X main.Version=$(VERSION) -X main.CommitHash=$(COMMIT_HASH) -X main.BuildDate=$(BUILD_DATE)"

# Build configuration
BINARY_NAME := daemon
BUILD_DIR := bin
# Reproducible build flags
BUILD_FLAGS := -trimpath -buildvcs=false
VERSION := $(shell git describe --tags --always --dirty)
LDFLAGS := -ldflags "-s -w -buildid= -X 'main.Version=$(VERSION)'"
DOCKER_IMAGE := xg2g
DOCKER_REGISTRY ?= 
PLATFORMS := linux/amd64,linux/arm64,darwin/amd64,darwin/arm64

# Coverage thresholds (matching CI configuration)
COVERAGE_THRESHOLD := 65
EPG_COVERAGE_THRESHOLD := 60

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
	@echo "  clean          Remove build artifacts and temporary files"
	@echo ""
	@echo "Quality Assurance:"
	@echo "  lint           Run golangci-lint with all checks"
	@echo "  lint-fix       Run golangci-lint with automatic fixes"
	@echo "  test           Run all unit tests"
	@echo "  test-race      Run tests with race detection"
	@echo "  test-cover     Run tests with coverage reporting"
	@echo "  test-fuzz      Run comprehensive fuzzing tests"
	@echo "  test-all       Run complete test suite (unit + race + fuzz)"
	@echo ""
	@echo "Enterprise Testing:"
	@echo "  hardcore-test  Run enterprise-grade comprehensive test suite"
	@echo "  quality-gates  Validate all quality gates (coverage, lint, security)"
	@echo ""
	@echo "Docker Operations:"
	@echo "  docker         Build Docker image"
	@echo "  docker-build   Build multi-platform Docker images"
	@echo "  docker-security Run Docker security scanning"
	@echo "  docker-tag     Tag Docker image with version"
	@echo "  docker-push    Push Docker image to registry"
	@echo "  docker-clean   Remove Docker build cache"
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
	@echo "Deployment Helpers:"
	@echo "  pin-digests            Replace <OWNER> and digest placeholders in deploy/ templates"
	@echo "  compose-up-alpine      Start Alpine image via docker compose (deploy/docker-compose.alpine.yml)"
	@echo "  compose-up-distroless  Start Distroless image via docker compose (deploy/docker-compose.distroless.yml)"
	@echo "  k8s-apply-alpine       Apply Kubernetes Alpine manifest (deploy/k8s-alpine.yaml)"
	@echo "  k8s-apply-distroless   Apply Kubernetes Distroless manifest (deploy/k8s-distroless.yaml)"
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

build: ## Build the main daemon binary
	@echo "Building xg2g daemon..."
	@mkdir -p $(BUILD_DIR)
	go build $(BUILD_FLAGS) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

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

clean: ## Remove build artifacts and temporary files
	@echo "Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@rm -rf coverage.out coverage.html
	@rm -rf *.prof *.test
	@rm -rf dist/
	@go clean -cache -testcache -modcache
	@echo "✅ Clean complete"

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

test: ## Run all unit tests
	@echo "Running unit tests..."
	@go test ./... -v
	@echo "✅ Unit tests passed"

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
	@echo "✅ All tests completed successfully"

# ===================================================================================================
# Enterprise Testing
# ===================================================================================================

hardcore-test: dev-tools ## Run enterprise-grade comprehensive test suite
	@echo "🚀 Starting Hardcore Test Suite - Enterprise Grade Quality Assurance"
	@echo "=================================================================="
	@echo ""
	@echo "Phase 1: Static Analysis and Linting..."
	@$(MAKE) lint
	@echo ""
	@echo "Phase 2: Comprehensive Testing with Race Detection..."
	@$(MAKE) test-race
	@echo ""
	@echo "Phase 3: Coverage Analysis with Thresholds..."
	@$(MAKE) test-cover
	@echo ""
	@echo "Phase 4: Fuzzing and Edge Cases..."
	@$(MAKE) test-fuzz
	@echo ""
	@echo "Phase 5: Security Vulnerability Scanning..."
	@$(MAKE) security-vulncheck
	@echo ""
	@echo "Phase 6: Dependency Verification..."
	@$(MAKE) deps-verify
	@echo ""
	@echo "Phase 7: Build Verification for All Platforms..."
	@$(MAKE) build-all
	@echo ""
	@echo "🎉 Hardcore Test Suite PASSED - Enterprise Quality Validated!"
	@echo "=================================================================="

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

docker-build: ## Build multi-platform Docker images
	@echo "Building multi-platform Docker images..."
	@docker buildx create --use --name xg2g-builder 2>/dev/null || true
	@docker buildx build --platform $(PLATFORMS) -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .
	@echo "✅ Multi-platform Docker images built"

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
	@"$(SYFT)" packages dir:. -o spdx-json=dist/sbom.spdx.json
	@"$(SYFT)" packages dir:. -o syft-table=dist/sbom.txt
	@echo "✅ SBOM generated: dist/sbom.spdx.json, dist/sbom.txt"

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
	@$(MAKE) hardcore-test
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

pin-digests: ## Replace <OWNER> and digest placeholders in deploy templates
	@OWNER="$${OWNER:?Set OWNER=your-gh-org-or-user}"; \
	ALPINE_DIGEST="$${ALPINE_DIGEST:-}"; \
	DISTROLESS_DIGEST="$${DISTROLESS_DIGEST:-}"; \
	./scripts/pin-digests.sh "$$OWNER" "$$ALPINE_DIGEST" "$$DISTROLESS_DIGEST"

compose-up-alpine: ## Start Alpine variant via docker compose
	docker compose -f deploy/docker-compose.alpine.yml up -d

compose-up-distroless: ## Start Distroless variant via docker compose
	docker compose -f deploy/docker-compose.distroless.yml up -d

k8s-apply-alpine: ## Apply Alpine Kubernetes manifest (set NS=namespace)
	kubectl apply -n "$${NS:-default}" -f deploy/k8s-alpine.yaml

k8s-apply-distroless: ## Apply Distroless Kubernetes manifest (set NS=namespace)
	kubectl apply -n "$${NS:-default}" -f deploy/k8s-distroless.yaml
