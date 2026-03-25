# ===================================================================================================
# Governance and Verification Gates
# ===================================================================================================

.PHONY: verify verify-generated-artifacts verify-generated-artifacts-contract verify-openapi-hard-mode verify-embedded-webui-dist verify-config verify-doc-links verify-capabilities contract-matrix verify-purity contract-freeze-check verify-no-sleep verify-no-panic verify-no-ignored-errors verify-determinism verify-codegen-transport verify-router-parity verify-oapi-codegen-version verify-no-hardcoded-baseurl verify-no-adhoc-terminal-mapping verify-no-adhoc-session-mapping verify-doc-image-tags verify-docs-compiled verify-digest-lock verify-release-policy verify-release-output-contract verify-runtime verify-hot-reload-governance verify-compose-resolver verify-systemd-runtime-contract verify-installation-contract gate-a gate-webui gate-repo-hygiene gate-v3-contract verify-v3-fanout

verify: verify-generated-artifacts verify-doc-links verify-capabilities contract-matrix verify-purity contract-freeze-check verify-no-sleep verify-no-panic verify-no-ignored-errors verify-determinism verify-codegen-transport verify-router-parity verify-oapi-codegen-version verify-no-hardcoded-baseurl verify-no-adhoc-terminal-mapping verify-no-adhoc-session-mapping verify-doc-image-tags verify-digest-lock verify-release-policy verify-release-output-contract verify-runtime verify-hot-reload-governance verify-compose-resolver verify-systemd-runtime-contract verify-installation-contract ## Run all governance verification gates

verify-config: ## Verify generated config surfaces are up-to-date
	@echo "Verifying generated config surfaces..."
	@cd $(BACKEND_DIR) && $(GO) run ./cmd/configgen
	@git diff --exit-code docs/guides/CONFIGURATION.md docs/guides/config.schema.json $(BACKEND_DIR)/config.generated.example.yaml docs/guides/CONFIG_SURFACES.md || (echo "❌ Config surfaces are out of sync. Run 'make generate-config' and commit changes." && exit 1)
	@echo "✅ Config surfaces are up-to-date"

verify-doc-links: ## Verify docs contains no broken relative links
	@if [ -f $(BACKEND_DIR)/scripts/verify-doc-links.sh ]; then \
		./$(BACKEND_DIR)/scripts/verify-doc-links.sh; \
	fi

verify-doc-image-tags: ## Verify Docker image tags are pinned and consistent
	@if [ -f $(BACKEND_DIR)/scripts/verify-doc-image-tags.sh ]; then \
		./$(BACKEND_DIR)/scripts/verify-doc-image-tags.sh; \
	fi

docs-render: ## Render templates into documentation and units
	@if [ -f $(BACKEND_DIR)/scripts/render-docs.sh ]; then \
		./$(BACKEND_DIR)/scripts/render-docs.sh; \
	fi

verify-docs-compiled: docs-render ## Verify that all docs and units are up-to-date
	@git diff --exit-code README.md deploy/docker-compose.yml docker-compose.yml infrastructure/docker/docker-compose.yml deploy/xg2g.service docs/ops/xg2g.service docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md docs/ops/OPERATIONS_MODEL.md docs/ops/xg2g-verifier.service docs/ops/xg2g-verifier.timer || (echo "❌ Documentation drift detected. Run 'make docs-render' and commit changes." && exit 1)
	@echo "✅ All documents and units are up-to-date"

verify-generated-artifacts-contract: ## Verify generated artifact governance coverage and ungoverned detection
	@./$(BACKEND_DIR)/scripts/verify-generated-artifacts-contract.sh

verify-openapi-hard-mode: ## Verify OpenAPI hard-mode generated artifacts are up-to-date
	@./$(BACKEND_DIR)/scripts/verify-openapi-hard-mode.sh

verify-embedded-webui-dist: ## Verify embedded WebUI dist is up-to-date
	@./$(BACKEND_DIR)/scripts/verify-embedded-webui-dist.sh

verify-generated-artifacts: verify-config verify-docs-compiled verify-generate verify-openapi-hard-mode verify-embedded-webui-dist verify-generated-artifacts-contract ## Verify all committed generated artifacts and governance rules
	@echo "✅ Generated artifact governance passed"

verify-release-output-contract: ## Verify the normative release/package output contract
	@./$(BACKEND_DIR)/scripts/verify-release-output-contract.sh

verify-purity: bootstrap-python-tools ## Verify UI purity, decision ownership, OpenAPI hygiene
	@./$(BACKEND_DIR)/scripts/verify-ui-purity.sh
	@./$(BACKEND_DIR)/scripts/verify-decision-ownership.sh
	@./$(BACKEND_DIR)/scripts/verify-openapi-hygiene.sh
	@$(PYTHON_TOOLS) ./$(BACKEND_DIR)/scripts/verify-openapi-no-duplicate-keys.py $(BACKEND_DIR)/api/openapi.yaml
	@./$(BACKEND_DIR)/scripts/ci_gate_adr_case.sh
	@./$(BACKEND_DIR)/scripts/ci_gate_storage_purity.sh
	@./$(BACKEND_DIR)/scripts/verify-openapi-lint.sh
	@./$(BACKEND_DIR)/scripts/verify-v3-shadowing.sh

contract-freeze-check: ## Verify contract goldens against baseline
	@./$(BACKEND_DIR)/scripts/verify-golden-freeze.sh

verify-no-sleep: ## Gate: No time.Sleep in production code
	@if grep -r "time.Sleep" $(BACKEND_DIR)/internal/ --include="*.go" | grep -v "_test.go" | grep -v "mock_server.go"; then \
		echo "❌ time.Sleep found in production code"; \
		exit 1; \
	fi
	@echo "✅ No production time.Sleep"

verify-no-panic: ## Gate: No panics in production code
	@if grep -rn "panic(" $(BACKEND_DIR)/internal/ --include="*.go" | grep -v "_test.go" | grep -vFf $(BACKEND_DIR)/panic_allowlist.txt; then \
		echo "❌ Panic found in production code"; \
		exit 1; \
	fi
	@echo "✅ No production panics"

verify-no-ignored-errors: ## Gate: No ignored errors
	@if grep -r "_ = err" $(BACKEND_DIR)/internal/ --include="*.go" | grep -v "_test.go"; then \
		echo "❌ Ignored error found"; \
		exit 1; \
	fi
	@echo "✅ No ignored errors"

gate-a: ## Gate A: Control Layer Store Purity
	@./$(BACKEND_DIR)/scripts/verify_gate_a_control_store.sh

gate-webui: ## Gate B: Thin-Client Audit
	@./$(BACKEND_DIR)/scripts/ci_gate_webui_audit.sh

gate-repo-hygiene: ## Gate C: Repository Hygiene
	@./$(BACKEND_DIR)/scripts/ci_gate_repo_hygiene.sh

gate-v3-contract: bootstrap-python-tools ## Gate V3: OpenAPI v3 Contract Governance
	@./$(BACKEND_DIR)/scripts/verify-openapi-lint.sh
	@$(PYTHON_TOOLS) ./$(BACKEND_DIR)/scripts/verify-openapi-no-duplicate-keys.py $(BACKEND_DIR)/api/openapi.yaml
	@./$(BACKEND_DIR)/scripts/verify-v3-shadowing.sh

verify-v3-fanout: ## Verify v3 package fan-out
	@./$(BACKEND_DIR)/scripts/check-fanout.sh

verify-compose-resolver: ## Verify compose resolver ordering and GPU-neutral base compose
	@./$(BACKEND_DIR)/scripts/verify-compose-resolver.sh

verify-systemd-runtime-contract: ## Verify systemd/runtime env contract semantics
	@./$(BACKEND_DIR)/scripts/verify-systemd-unit.sh
	@./$(BACKEND_DIR)/scripts/verify-systemd-runtime-contract.sh

verify-installation-contract: ## Verify packaging/install-time host layout contract
	@./$(BACKEND_DIR)/scripts/verify-installation-contract.sh
