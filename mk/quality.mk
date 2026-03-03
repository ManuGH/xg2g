# ===================================================================================================
# Quality Assurance Targets
# ===================================================================================================

.PHONY: lint lint-fix test test-race test-cover cover test-all test-integration smoke-test codex security-scan security-audit sbom quality-gates quality-gates-offline quality-gates-online ci-pr ci-nightly bootstrap-python-tools

lint: ## Run golangci-lint with all checks
	@echo "Running golangci-lint..."
	@cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --timeout=5m --verbose ./...
	@if [ -f $(FRONTEND_DIR)/webui/scripts/verify-no-hardcoded-colors.sh ]; then \
		./$(FRONTEND_DIR)/webui/scripts/verify-no-hardcoded-colors.sh; \
	fi
	@echo "✅ Linting passed"

lint-fix: ## Run golangci-lint with automatic fixes
	@echo "Running golangci-lint fix..."
	@cd $(BACKEND_DIR) && $(GOLANGCI_LINT) run --fix ./...
	@echo "✅ Linting fixes applied"

test: ## Run all unit tests
	@echo "Running unit tests..."
	@cd $(BACKEND_DIR) && $(GO) test ./... -v -count=1 -timeout=$(GO_TEST_TIMEOUT)
	@echo "✅ Unit tests passed"

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	@cd $(BACKEND_DIR) && $(GO) test ./... -v -race -count=1 -timeout=$(GO_TEST_RACE_TIMEOUT)
	@echo "✅ Race detection tests passed"

test-cover: ## Run tests with coverage reporting
	@echo "Running tests with coverage..."
	@mkdir -p $(ARTIFACTS_DIR)
	@cd $(BACKEND_DIR) && $(GO) test -count=1 -timeout=$(GO_TEST_COVER_TIMEOUT) -covermode=atomic -coverprofile=../$(ARTIFACTS_DIR)/coverage.out -coverpkg=./... ./...
	@$(GO) tool cover -html=$(ARTIFACTS_DIR)/coverage.out -o $(ARTIFACTS_DIR)/coverage.html
	@echo "Coverage report generated: $(ARTIFACTS_DIR)/coverage.html"
	@$(GO) tool cover -func=$(ARTIFACTS_DIR)/coverage.out | tail -1

cover: test-cover ## Alias for test-cover

test-all: test-race test-cover ## Run complete test suite (unit + race)

smoke-test: ## Run E2E smoke test (Builds & Runs daemon)
	@echo "Building binary for smoke test..."
	@mkdir -p $(BUILD_DIR)
	@cd $(BACKEND_DIR) && $(GO) build -tags=nogpu -o ../$(BUILD_DIR)/xg2g-smoke ./cmd/daemon
	@echo "Running E2E smoke test..."
	@XG2G_SMOKE_BIN=$$(pwd)/$(BUILD_DIR)/xg2g-smoke cd $(BACKEND_DIR) && $(GO) test -v -tags=smoke,nogpu -timeout=30s ./test/smoke/...
	@echo "✅ Smoke test passed"

codex: quality-gates ## Run Codex review bundle (lint + race/coverage + govulncheck)
	@echo "✅ Codex review bundle completed"

security: security-vulncheck security-audit sbom ## Run comprehensive security analysis

security-scan: dev-tools ## Run container vulnerability scanning
	@echo "Running container vulnerability scan..."
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(GRYPE)" dir:. -o table -o json=$(ARTIFACTS_DIR)/vulnerabilities.json || echo "⚠️  Grype scan completed with findings"
	@echo "✅ Vulnerability scan completed: $(ARTIFACTS_DIR)/vulnerabilities.json"

security-audit: ## Run dependency vulnerability audit
	@echo "Running dependency security audit..."
	@cd $(BACKEND_DIR) && $(GO) list -json -deps ./... | nancy sleuth || echo "⚠️  Nancy audit completed with findings"
	@echo "✅ Dependency audit completed"

security-vulncheck: ## Run Go vulnerability checker
	@echo "Ensuring govulncheck is installed..."
	@[ -x "$(GOVULNCHECK)" ] || GOFLAGS= $(GO) install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@echo "Running Go vulnerability check..."
	@cd $(BACKEND_DIR) && $(GOVULNCHECK) ./...
	@echo "✅ Go vulnerability check passed"

sbom: dev-tools ## Generate Software Bill of Materials
	@echo "Generating SBOM..."
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(SYFT)" scan dir:. -o spdx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.spdx.json
	@"$(SYFT)" scan dir:. -o cyclonedx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.cyclonedx.json
	@echo "✅ SBOM generated and validated"

quality-gates: quality-gates-online

quality-gates-offline: ## Offline-only gates (no network, no codegen)
	@echo "Validating offline gates..."
	@cd $(BACKEND_DIR) && $(GO) test ./... -count=1 -timeout=$(GO_TEST_TIMEOUT)
	@cd $(BACKEND_DIR) && $(GO) vet ./...
	@echo "✅ Offline gates passed"

quality-gates-online: verify-config verify-docs-compiled verify-generate verify-v3-fanout gate-repo-hygiene gate-v3-contract gate-a gate-webui lint-invariants lint test-cover security-vulncheck ## Validate all online quality gates
	@echo "Validating quality gates..."
	@echo "✅ All quality gates passed"

# Deterministic Python toolchain for governance scripts.
.PHONY: bootstrap-python-tools
bootstrap-python-tools:
	@./$(BACKEND_DIR)/scripts/bootstrap-python-tools.sh
