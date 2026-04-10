# ===================================================================================================
# Backend Targets
# ===================================================================================================

.PHONY: backend-build backend-run generate-config verify-config generate verify-generate gen-openapi-hard ui-build build build-with-ui build-offline backend-dev backend-dev-ui webui-dev dev-ui dev android-local-smoke android-tv-smoke install doctor check-tools dev-tools

ui-build: ## Build WebUI assets
	@echo "Building WebUI assets..."
	@cd $(FRONTEND_DIR)/webui && npm ci && npm run build
	@echo "Copying to $(WEBUI_DIST_DIR)..."
	@rm -rf "$(WEBUI_DIST_DIR)"
	@mkdir -p "$(WEBUI_DIST_DIR)"
	@cp -R $(FRONTEND_DIR)/webui/dist/. "$(WEBUI_DIST_DIR)"/
	@echo "✅ WebUI build complete"

build: ## Build xg2g binary (Go-only, offline-safe)
	@echo "▶ build (Go-only, offline-safe)"
	@mkdir -p $(BUILD_DIR)
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && \
		GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOVCS="*:off" "$$GO_BIN" build $(BUILD_FLAGS) $(LDFLAGS) -mod=vendor -o ../$(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-with-ui: ui-build ## Build WebUI assets and then build the daemon
	@echo "▶ build-with-ui (requires Node)"
	@$(MAKE) build

generate: ## Generate Go code from OpenAPI spec (v3 only)
	@echo "Generating API server code (v3)..."
	@mkdir -p $(BACKEND_DIR)/internal/api
	@mkdir -p $(BACKEND_DIR)/internal/control/http/v3
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-api.yaml -o internal/api/server_gen.go api/openapi.yaml
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-v3.yaml -o internal/control/http/v3/server_gen.go api/openapi.yaml
	@echo "✅ Code generation complete (single source: api/openapi.yaml):"
	@echo "   - $(BACKEND_DIR)/internal/control/http/v3/server_gen.go"

verify-generate: generate ## Verify that generated code is up-to-date
	@echo "Verifying generated code..."
	@cd $(BACKEND_DIR) && git diff --exit-code internal/api/server_gen.go internal/control/http/v3/server_gen.go || (echo "❌ Generated code is out of sync. Run 'make generate' and commit changes." && exit 1)
	@echo "✅ Generated code is up-to-date"

gen-openapi-hard: ## Generate OpenAPI hard-mode artifacts (snapshot + TS client + consumption types)
	@cd $(BACKEND_DIR) && ./scripts/gen-openapi-hard-mode.sh

generate-config: ## Generate config surfaces from registry
	@echo "Generating config surfaces from registry..."
	@cd $(BACKEND_DIR) && $(RESOLVE_GO_BIN_SH) && GOTOOLCHAIN=local "$$GO_BIN" run ./cmd/configgen --allow-create
	@echo "✅ Config surfaces generated"

backend-dev: ## Run the backend in the foreground (advanced, no containers)
	@echo "Starting xg2g backend in the foreground..."
	@./$(BACKEND_DIR)/scripts/run-local-backend.sh

backend-dev-ui: ## Run the dev-tagged backend in the foreground on http://localhost:8080/ui/ (advanced)
	@echo "Starting xg2g backend with the dev UI proxy on http://localhost:8080/ui/ ..."
	@./$(BACKEND_DIR)/scripts/run-local-backend.sh --ui

webui-dev: ## Start only the Vite dev server (advanced, second terminal)
	@cd $(FRONTEND_DIR)/webui && \
	if [ ! -d node_modules ]; then \
		npm ci; \
	fi && \
	npm run dev

dev-ui: ## Run the frontend HMR path (Vite background + foreground backend on :8080)
	@./run_ui_dev.sh

android-local-smoke: ## Build embedded UI, run local backend on :8080, and launch Android dev app with auth if adb is connected
	@./run_android_local.sh

android-tv-smoke: ## Run the paired-device Android TV smoke with backend, guide, playback, and WebUI assertions
	@./run_android_tv_smoke.sh

dev: ## Run the crash-restart development loop (advanced/internal)
	@./run_dev.sh

install: ## Bootstrap local developer workspace (.env + WebUI dependencies)
	@./$(BACKEND_DIR)/scripts/dev-install.sh

doctor: ## Verify the local developer workspace
	@$(MAKE) check-tools

check-tools: ## Verify required local developer tools and workspace state
	@./$(BACKEND_DIR)/scripts/check-dev-setup.sh

dev-tools: ## Install pinned local developer CLI tools
	@echo "Installing pinned Go developer tools..."
	@$(RESOLVE_GO_BIN_SH) && GOFLAGS= GOWORK=off GOTOOLCHAIN=local "$$GO_BIN" install $(GOLANGCI_LINT_MODULE)@$(GOLANGCI_LINT_VERSION)
	@$(RESOLVE_GO_BIN_SH) && GOFLAGS= GOWORK=off GOTOOLCHAIN=local "$$GO_BIN" install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	@$(MAKE) bootstrap-python-tools
	@echo "✅ Developer CLI tools ready"
