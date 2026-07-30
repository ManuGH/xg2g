# ===================================================================================================
# Governance and Verification Gates
# ===================================================================================================

.PHONY: verify verify-generated-artifacts verify-generated-artifacts-contract verify-openapi-hard-mode verify-embedded-webui-dist verify-client-ts-fresh verify-webui-router-security verify-config verify-doc-links verify-capabilities contract-matrix verify-purity contract-freeze-check verify-no-sleep verify-no-panic verify-no-ignored-errors verify-determinism verify-codegen-transport verify-router-parity verify-oapi-codegen-version verify-no-hardcoded-baseurl verify-no-adhoc-terminal-mapping verify-no-adhoc-session-mapping verify-doc-image-tags verify-docs-compiled verify-digest-lock verify-release-policy verify-release-output-contract verify-runtime verify-runtime-contract verify-hot-reload-governance verify-compose-resolver verify-monitoring-contract verify-scheduled-ci-contract verify-start-surface verify-systemd-runtime-contract verify-installation-contract verify-linux-setup-wizard verify-linux-lifecycle verify-maintainer-deploy-topology verify-public-deployment verify-capacity-autocodec-demotion verify-codec-path-matrix gate-a gate-webui gate-repo-hygiene gate-v3-contract verify-v3-fanout verify-dead-packages

verify: verify-generated-artifacts verify-webui-router-security verify-doc-links verify-capabilities contract-matrix verify-purity contract-freeze-check verify-no-sleep verify-no-panic verify-no-ignored-errors verify-determinism verify-codegen-transport verify-router-parity verify-oapi-codegen-version verify-no-hardcoded-baseurl verify-no-adhoc-terminal-mapping verify-no-adhoc-session-mapping verify-no-hls-startup-policy-client-usage verify-doc-image-tags verify-digest-lock verify-release-policy verify-release-output-contract verify-runtime-contract verify-hot-reload-governance verify-compose-resolver verify-monitoring-contract verify-scheduled-ci-contract verify-start-surface verify-systemd-runtime-contract verify-installation-contract verify-linux-setup-wizard verify-linux-lifecycle ## Run all hermetic repository governance gates

verify-config: ## Verify generated config surfaces are up-to-date
	@echo "Verifying generated config surfaces..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" run ./cmd/configgen
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
	@git diff --exit-code README.md infra/systemd/docker-compose.yml infra/systemd/xg2g.service docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md docs/ops/OPERATIONS_MODEL.md docs/ops/xg2g-verifier.service docs/ops/xg2g-verifier.timer || (echo "❌ Documentation drift detected. Run 'make docs-render' and commit changes." && exit 1)
	@echo "✅ All documents and units are up-to-date"

verify-generated-artifacts-contract: ## Verify generated artifact governance coverage and ungoverned detection
	@./$(BACKEND_DIR)/scripts/verify-generated-artifacts-contract.sh

verify-openapi-hard-mode: ## Verify OpenAPI hard-mode generated artifacts are up-to-date
	@./$(BACKEND_DIR)/scripts/verify-openapi-hard-mode.sh

verify-embedded-webui-dist: ## Verify embedded WebUI dist is up-to-date
	@./$(BACKEND_DIR)/scripts/verify-embedded-webui-dist.sh

verify-client-ts-fresh: ## Verify generated TS API client is up-to-date with openapi.yaml
	@MAKE="$(MAKE)" ./$(BACKEND_DIR)/scripts/verify-client-ts-fresh.sh

verify-webui-router-security: ## Pin the fixed React Router and keep the WebUI on its reviewed declarative surface
	@./$(BACKEND_DIR)/scripts/verify-webui-router-security.sh

verify-generated-artifacts: verify-config verify-docs-compiled verify-generate verify-openapi-hard-mode verify-embedded-webui-dist verify-client-ts-fresh verify-generated-artifacts-contract ## Verify all committed generated artifacts and governance rules
	@echo "✅ Generated artifact governance passed"

verify-release-output-contract: ## Verify the normative release/package output contract
	@./$(BACKEND_DIR)/scripts/verify-release-output-contract.sh

verify-public-deployment: ## Verify live public web/native contract against a running deployment
	@./$(BACKEND_DIR)/scripts/verify-public-deployment.sh

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
	@cd $(BACKEND_DIR) && ./scripts/verify-no-panic.sh

verify-no-ignored-errors: ## Gate: No ignored errors
	@if grep -r "_ = err" $(BACKEND_DIR)/internal/ --include="*.go" | grep -v "_test.go"; then \
		echo "❌ Ignored error found"; \
		exit 1; \
	fi
	@echo "✅ No ignored errors"

verify-determinism: ## Verify deterministic contract behavior
	@cd $(BACKEND_DIR) && ./scripts/verify-determinism.sh

verify-codegen-transport: ## Verify generated transport remains policy-free
	@cd $(BACKEND_DIR) && ./scripts/verify-codegen-transport-only.sh

verify-router-parity: ## Verify generated and handwritten router parity
	@cd $(BACKEND_DIR) && ./scripts/verify-router-parity.sh

verify-oapi-codegen-version: ## Verify the pinned OpenAPI generator version
	@cd $(BACKEND_DIR) && ./scripts/verify-oapi-codegen-version.sh

verify-no-hardcoded-baseurl: ## Verify v3 handlers do not hardcode the base URL
	@cd $(BACKEND_DIR) && ./scripts/verify-no-hardcoded-baseurl.sh

verify-no-adhoc-terminal-mapping: ## Verify terminal-state mapping ownership
	@cd $(BACKEND_DIR) && ./scripts/verify-no-adhoc-terminal-mapping.sh

verify-no-adhoc-session-mapping: ## Verify session-state mapping ownership
	@cd $(BACKEND_DIR) && ./scripts/verify-no-adhoc-session-mapping.sh

verify-digest-lock: ## Verify release image digest governance
	@./$(BACKEND_DIR)/scripts/verify-digest-lock.sh

verify-release-policy: ## Verify release policy
	@./$(BACKEND_DIR)/scripts/verify-release-policy.sh

verify-runtime: ## Verify a running installed deployment against repo truth
	@./$(BACKEND_DIR)/scripts/verify-runtime.sh

verify-runtime-contract: ## Verify the live runtime verifier fails closed
	@./$(BACKEND_DIR)/scripts/verify-runtime-contract.sh

verify-hot-reload-governance: ## Verify hot-reload ownership boundaries
	@cd $(BACKEND_DIR) && $(GO) run ./scripts/verify-hot-reload-governance.go

gate-a: ## Gate A: Control Layer Store Purity
	@./$(BACKEND_DIR)/scripts/verify_gate_a_control_store.sh

gate-webui: ## Gate B: Thin-Client Audit
	@./$(BACKEND_DIR)/scripts/ci_gate_webui_audit.sh
	@./$(FRONTEND_DIR)/webui/scripts/verify-no-hls-startup-policy-client-usage.sh

verify-no-hls-startup-policy-client-usage: ## Verify HLS startup policy debug fields do not leak into product WebUI policy code
	@./$(FRONTEND_DIR)/webui/scripts/verify-no-hls-startup-policy-client-usage.sh

gate-repo-hygiene: ## Local wrapper for repository health checks; GitHub repo-health.yml is authoritative
	@./$(BACKEND_DIR)/scripts/ci_gate_root_purity.sh
	@./$(BACKEND_DIR)/scripts/ci/check-large-files.sh
	@./$(BACKEND_DIR)/scripts/ci/check-test-assets-location.sh
	@./$(BACKEND_DIR)/scripts/ci_gate_repo_hygiene.sh

gate-v3-contract: bootstrap-python-tools ## Gate V3: OpenAPI v3 Contract Governance
	@./$(BACKEND_DIR)/scripts/verify-openapi-lint.sh
	@$(PYTHON_TOOLS) ./$(BACKEND_DIR)/scripts/verify-openapi-no-duplicate-keys.py $(BACKEND_DIR)/api/openapi.yaml
	@./$(BACKEND_DIR)/scripts/verify-v3-shadowing.sh

verify-v3-fanout: ## Verify v3 package fan-out
	@./$(BACKEND_DIR)/scripts/check-fanout.sh

verify-dead-packages: ## Gate: block new unimported (whole-package dead) internal packages
	@./$(BACKEND_DIR)/scripts/check-dead-packages.sh

verify-compose-resolver: ## Verify compose resolver ordering and GPU-neutral base compose
	@./$(BACKEND_DIR)/scripts/verify-compose-resolver.sh

verify-monitoring-contract: ## Verify monitoring topology, provisioning, and SLO rules
	@./$(BACKEND_DIR)/scripts/verify-monitoring-contract.sh

verify-scheduled-ci-contract: ## Verify one nightly owner and the enforced performance lane
	@./$(BACKEND_DIR)/scripts/verify-scheduled-ci-contract.sh

verify-start-surface: ## Verify the public development and production start boundaries
	@./$(BACKEND_DIR)/scripts/verify-start-surface.sh

verify-systemd-runtime-contract: ## Verify systemd/runtime env contract semantics
	@./$(BACKEND_DIR)/scripts/verify-systemd-unit.sh
	@./$(BACKEND_DIR)/scripts/verify-systemd-runtime-contract.sh

verify-installation-contract: ## Verify packaging/install-time host layout contract
	@./$(BACKEND_DIR)/scripts/verify-installation-contract.sh

verify-linux-setup-wizard: ## Verify beginner-safe Linux first-run setup
	@./$(BACKEND_DIR)/scripts/verify-linux-setup-wizard.sh

verify-linux-lifecycle: ## Verify beginner-safe backup, restore, diagnostics, and removal
	@./$(BACKEND_DIR)/scripts/verify-linux-lifecycle.sh

verify-maintainer-deploy-topology: ## Verify staging/build paths do not use runtime checkouts
	@./$(BACKEND_DIR)/scripts/verify-maintainer-deploy-topology.sh

verify-capacity-autocodec-demotion: ## Verify deterministic auto-codec capacity/demotion behavior
	@./$(BACKEND_DIR)/scripts/verify-capacity-autocodec-demotion.sh

verify-codec-path-matrix: ## Verify x264/x265/AV1 codec-path matrix and iOS codec-path policy
	@./$(BACKEND_DIR)/scripts/verify-codec-path-matrix.sh
