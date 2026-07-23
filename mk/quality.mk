# ===================================================================================================
# Quality Assurance Targets
# ===================================================================================================

.PHONY: lint lint-fix test test-race test-race-pr test-cover cover test-all test-integration smoke-test codex security security-closure security-scan security-audit sbom quality-gates quality-gates-offline quality-gates-online lint-invariants verify-client-wrapper webui-test webui-browser-smoke ci-pr ci-nightly bootstrap-python-tools pre-push hooks-install

pre-push: ## Fast local guard against the PR gate's cheapest failures (gofmt, vet, build) — seconds, not minutes
	@echo "Checking gofmt..."
	@UNFMT=$$(cd $(BACKEND_DIR) && git ls-files -- '*.go' ':(exclude)vendor/**' | xargs gofmt -l 2>/dev/null); if [ -n "$$UNFMT" ]; then \
		echo "❌ Not gofmt'd (run: cd $(BACKEND_DIR) && gofmt -w <files>):"; echo "$$UNFMT"; exit 1; fi
	@echo "Running go vet..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" vet ./...
	@echo "Building..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" build ./...
	@echo "✅ pre-push guard passed"

hooks-install: ## Install the versioned git pre-push hook into .git/hooks
	@HOOKS_DIR=$$(git rev-parse --git-path hooks) && \
		mkdir -p "$$HOOKS_DIR" && \
		cp scripts/git-hooks/pre-push "$$HOOKS_DIR/pre-push" && \
		chmod +x "$$HOOKS_DIR/pre-push" && \
		echo "✅ pre-push hook installed to $$HOOKS_DIR"

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

lint-invariants: ## Run repository lint invariants enforced in CI
	@echo "Running lint invariants..."
	@echo "Scanning for direct environment access outside internal/config..."
	@VIOLATIONS=$$(grep -RIn --exclude-dir=vendor --exclude-dir=node_modules --exclude-dir=.git --include='*.go' --exclude='*_test.go' -E '\b(os|syscall)\.(Getenv|LookupEnv|Environ|ExpandEnv|Expand)\b' $(BACKEND_DIR)/internal/ $(BACKEND_DIR)/cmd/ | grep -vE '^$(BACKEND_DIR)/internal/config/' || true); \
	if [ -n "$$VIOLATIONS" ]; then \
		echo "❌ Direct environment access detected outside authorized packages:"; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@echo "Scanning for naked TODO/FIXME comments without (reference)..."
	@VIOLATIONS=$$(grep -RIn --exclude-dir=vendor --exclude-dir=node_modules --exclude-dir=.git --include='*.go' --include='*.ts' --include='*.tsx' -E '^[[:space:]]*//[[:space:]]*(TODO|FIXME)([^a-zA-Z0-9(]|$$)' $(BACKEND_DIR)/ $(FRONTEND_DIR)/webui/src/ | grep -vE '(\.gen\.ts|_gen\.go|\.spec\.|\_test\.go)' || true); \
	if [ -n "$$VIOLATIONS" ]; then \
		echo "❌ Naked TODO/FIXME detected without reference parentheses (e.g. // TODO(reference): ...):"; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@$(PYTHON) $(BACKEND_DIR)/scripts/check_deprecations.py
	@cd $(FRONTEND_DIR)/webui && npm run lint
	@echo "✅ Lint invariants passed"

test: ## Run all unit tests
	@echo "Running unit tests..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test ./... -v -count=1 -timeout=$(GO_TEST_TIMEOUT)
	@echo "✅ Unit tests passed"

test-race: ## Run tests with race detection
	@echo "Running tests with race detection..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test ./... -v -race -count=1 -timeout=$(GO_TEST_RACE_TIMEOUT)
	@echo "✅ Race detection tests passed"

test-race-pr: ## Race detector scoped to live concurrency-critical packages (PR gate; deterministic, no ffmpeg binary)
	@echo "Running scoped race detector (PR gate)..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test -race -count=1 -timeout=$(GO_TEST_RACE_TIMEOUT) \
		./internal/domain/session/... \
		./internal/pipeline/bus/... \
		./internal/infra/media/ffmpeg/... \
		./internal/procgroup/... \
		./internal/openwebif/...
	@echo "✅ Scoped race detection (PR gate) passed"

test-cover: ## Run tests with coverage reporting
	@echo "Running tests with coverage..."
	@mkdir -p $(ARTIFACTS_DIR)
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && { \
	  GOTOOLCHAIN=local "$$GO_BIN" test -count=1 -timeout=$(GO_TEST_COVER_TIMEOUT) -covermode=atomic -coverprofile=$(CURDIR)/$(ARTIFACTS_DIR)/coverage.out -coverpkg=./internal/...,./test/... ./... && \
	  GOTOOLCHAIN=local "$$GO_BIN" tool cover -html=$(CURDIR)/$(ARTIFACTS_DIR)/coverage.out -o $(CURDIR)/$(ARTIFACTS_DIR)/coverage.html && \
	  echo "Coverage report generated: $(ARTIFACTS_DIR)/coverage.html" && \
	  GOTOOLCHAIN=local "$$GO_BIN" tool cover -func=$(CURDIR)/$(ARTIFACTS_DIR)/coverage.out | tail -1; \
	}

cover: test-cover ## Alias for test-cover

test-all: test-race test-cover ## Run complete test suite (unit + race)

smoke-test: ## Run E2E smoke test (Builds & Runs daemon)
	@echo "Building binary for smoke test..."
	@mkdir -p $(BUILD_DIR)
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" build -tags=nogpu -o ../$(BUILD_DIR)/xg2g-smoke ./cmd/daemon
	@echo "Running E2E smoke test..."
	@XG2G_SMOKE_BIN=$$(pwd)/$(BUILD_DIR)/xg2g-smoke cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test -v -tags=smoke,nogpu -timeout=30s ./test/smoke/...
	@echo "✅ Smoke test passed"

verify-client-wrapper: ## Verify WebUI generated client boundary rules
	@echo "Verifying WebUI client wrapper boundary..."
	@cd $(FRONTEND_DIR)/webui && npm run verify:client-wrapper
	@echo "✅ WebUI client wrapper boundary passed"

webui-test: ## Run WebUI unit tests
	@echo "Running WebUI unit tests..."
	@cd $(FRONTEND_DIR)/webui && npm run test
	@echo "✅ WebUI unit tests passed"

webui-browser-smoke: ## Run the Playwright browser smoke against the fixture-backed WebUI
	@echo "Running WebUI browser smoke..."
	@[ -d node_modules/@playwright/test ] || npm ci
	@[ -d $(FRONTEND_DIR)/webui/node_modules/vite ] || (cd $(FRONTEND_DIR)/webui && npm ci)
	@[ -d $(BACKEND_DIR)/e2e/fixture-server/node_modules/fastify ] || (cd $(BACKEND_DIR)/e2e/fixture-server && npm ci)
	@bash $(BACKEND_DIR)/e2e/scripts/gen-hls-fixture.sh
	@npx playwright test --config $(BACKEND_DIR)/e2e/playwright.config.ts --project=chromium
	@echo "✅ WebUI browser smoke passed"

codex: quality-gates ## Run Codex review bundle (lint + race/coverage + govulncheck)
	@echo "✅ Codex review bundle completed"

security: security-vulncheck security-audit sbom ## Run comprehensive security analysis

security-closure: ## Rebuild images fresh and run local security closure proofs
	@echo "Running confinement regression matrix..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test ./internal/platform/fs -count=1 -run 'TestConfine(RelPath|AbsPath)' -v
	@echo "Running container runtime closure proof..."
	@./$(BACKEND_DIR)/scripts/verify-image-runtime.sh
	@echo "NOTE: Re-run GitHub CodeQL, Trivy, and Scorecard workflows to close scanner evidence."

security-scan: ## Run container vulnerability scanning
	@echo "Running container vulnerability scan..."
	@command -v "$(GRYPE)" >/dev/null 2>&1 || (echo "❌ grype not found. Install it before running 'make security-scan'." && exit 1)
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(GRYPE)" dir:. -o table -o json=$(ARTIFACTS_DIR)/vulnerabilities.json || echo "⚠️  Grype scan completed with findings"
	@echo "✅ Vulnerability scan completed: $(ARTIFACTS_DIR)/vulnerabilities.json"

security-audit: ## Run dependency vulnerability audit
	@echo "Running dependency security audit..."
	@command -v nancy >/dev/null 2>&1 || (echo "❌ nancy not found. Install it before running 'make security-audit'." && exit 1)
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" list -json -deps ./... | nancy sleuth || echo "⚠️  Nancy audit completed with findings"
	@echo "✅ Dependency audit completed"

security-vulncheck: ## Run Go vulnerability checker
	@echo "Ensuring govulncheck is installed..."
	@[ -x "$(GOVULNCHECK)" ] || ($(RESOLVE_GO_BIN_SH) && GOFLAGS= GOTOOLCHAIN=local "$$GO_BIN" install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION))
	@echo "Running Go vulnerability check..."
	@cd $(BACKEND_DIR) && $(GOVULNCHECK) ./...
	@echo "✅ Go vulnerability check passed"

sbom: ## Generate Software Bill of Materials
	@echo "Generating SBOM..."
	@command -v "$(SYFT)" >/dev/null 2>&1 || (echo "❌ syft not found. Install it before running 'make sbom'." && exit 1)
	@mkdir -p $(ARTIFACTS_DIR)
	@"$(SYFT)" scan dir:. -o spdx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.spdx.json
	@"$(SYFT)" scan dir:. -o cyclonedx-json --source-name xg2g --source-version $(VERSION) > $(ARTIFACTS_DIR)/sbom.cyclonedx.json
	@echo "✅ SBOM generated and validated"

quality-gates: quality-gates-online

quality-gates-offline: ## Offline-only gates (no network, no codegen)
	@echo "Validating offline gates..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test ./... -count=1 -timeout=$(GO_TEST_TIMEOUT)
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" vet ./...
	@echo "✅ Offline gates passed"

quality-gates-online: verify-generated-artifacts verify-release-output-contract verify-v3-fanout verify-dead-packages verify-compose-resolver verify-systemd-runtime-contract verify-installation-contract verify-capacity-autocodec-demotion verify-codec-path-matrix gate-repo-hygiene gate-v3-contract gate-a gate-webui lint-invariants lint test-cover security-vulncheck ## Validate all online quality gates
	@echo "Validating quality gates..."
	@echo "✅ All quality gates passed"

ci-pr: lint verify-generated-artifacts verify-release-output-contract verify-compose-resolver verify-start-surface verify-systemd-runtime-contract verify-installation-contract verify-capacity-autocodec-demotion verify-codec-path-matrix verify-client-wrapper webui-test test-pr ## PR gate check bundle (fast & deterministic)
	@echo "✅ PR gate bundle passed"

test-pr: ## Run deterministic unit tests for PR gate (short mode, no binary dependencies)
	@echo "Running PR gate unit tests..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" test -short ./... -count=1 -timeout=$(GO_TEST_TIMEOUT)
	@echo "✅ PR gate unit tests passed"

ci-nightly: quality-gates-online webui-test ## Run the local deep validation bundle
	@echo "✅ Nightly gate bundle passed"

# Deterministic Python toolchain for governance scripts.
.PHONY: bootstrap-python-tools
bootstrap-python-tools:
	@./$(BACKEND_DIR)/scripts/bootstrap-python-tools.sh
