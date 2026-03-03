# ===================================================================================================
# Backend Targets
# ===================================================================================================

.PHONY: backend-build backend-run generate-config verify-config generate verify-generate ui-build backend-dev dev

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
	@cd $(BACKEND_DIR) && GOTOOLCHAIN=local GOPROXY=off GOSUMDB=off GOVCS="*:off" $(GO) build $(BUILD_FLAGS) $(LDFLAGS) -mod=vendor -o ../$(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon
	@echo "✅ Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

build-with-ui: ui-build ## Build WebUI assets and then build the daemon
	@echo "▶ build-with-ui (requires Node)"
	@$(MAKE) build

generate: ## Generate Go code from OpenAPI spec (v3 only)
	@echo "Generating API server code (v3)..."
	@mkdir -p $(BACKEND_DIR)/internal/api
	@mkdir -p $(BACKEND_DIR)/internal/control/http/v3
	@cd $(BACKEND_DIR) && $(GO) run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-api.yaml -o internal/api/server_gen.go api/openapi.yaml
	@cd $(BACKEND_DIR) && $(GO) run -mod=vendor github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen -config api/oapi-codegen-v3.yaml -o internal/control/http/v3/server_gen.go api/openapi.yaml
	@echo "✅ Code generation complete (single source: api/openapi.yaml):"
	@echo "   - $(BACKEND_DIR)/internal/control/http/v3/server_gen.go"

verify-generate: generate ## Verify that generated code is up-to-date
	@echo "Verifying generated code..."
	@cd $(BACKEND_DIR) && git diff --exit-code internal/api/server_gen.go internal/control/http/v3/server_gen.go || (echo "❌ Generated code is out of sync. Run 'make generate' and commit changes." && exit 1)
	@echo "✅ Generated code is up-to-date"

generate-config: ## Generate config surfaces from registry
	@echo "Generating config surfaces from registry..."
	@cd $(BACKEND_DIR) && $(GO) run ./cmd/configgen --allow-create
	@echo "✅ Config surfaces generated"

backend-dev: ## Run daemon locally for development (once)
	@echo "Starting xg2g in development mode..."
	@cd $(BACKEND_DIR) && \
	if [ ! -f .env ]; then \
		cp .env.example .env; \
	fi && \
	set -a; . ./.env; set +a; \
	$(GO) build $(BUILD_FLAGS) $(LDFLAGS) -o ../$(BUILD_DIR)/$(BINARY_NAME) ./cmd/daemon && \
	../$(BUILD_DIR)/$(BINARY_NAME)

dev: ## Run development loop
	@./run_dev.sh
