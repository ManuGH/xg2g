# ===================================================================================================
# Operations and Orchestration
# ===================================================================================================

.PHONY: start stop start-gpu stop-gpu start-nvidia stop-nvidia up down status ps logs restart prod-up prod-down prod-ps prod-logs prod-restart release-check release changelog setup build-ffmpeg repair-metadata runtime-sync-check runtime-sync-apply

start: doctor ## Start the default local container stack on http://localhost:8088
	@./$(BACKEND_DIR)/scripts/check-local-runtime.sh base
	@$(MAKE) up

stop: ## Stop the default local container stack
	@$(MAKE) down

start-gpu: doctor ## Start the local container stack with /dev/dri render-node passthrough (Linux)
	@./$(BACKEND_DIR)/scripts/check-local-runtime.sh vaapi
	@XG2G_COMPOSE_ROOT="$(CURDIR)/deploy" \
	XG2G_ENV_FILE="$(CURDIR)/.env" \
	COMPOSE_FILE="docker-compose.yml:../docker-compose.dev.yml:docker-compose.gpu.yml" \
	./$(BACKEND_DIR)/scripts/compose-xg2g.sh up -d
	@echo "✅ GPU stack started at http://localhost:8088"

stop-gpu: ## Stop the /dev/dri-backed local container stack
	@XG2G_COMPOSE_ROOT="$(CURDIR)/deploy" \
	XG2G_ENV_FILE="$(CURDIR)/.env" \
	COMPOSE_FILE="docker-compose.yml:../docker-compose.dev.yml:docker-compose.gpu.yml" \
	./$(BACKEND_DIR)/scripts/compose-xg2g.sh down
	@echo "✅ GPU stack stopped"

start-nvidia: doctor ## Start the local container stack with the NVIDIA overlay
	@./$(BACKEND_DIR)/scripts/check-local-runtime.sh nvidia
	@XG2G_COMPOSE_ROOT="$(CURDIR)/deploy" \
	XG2G_ENV_FILE="$(CURDIR)/.env" \
	COMPOSE_FILE="docker-compose.yml:../docker-compose.dev.yml:docker-compose.nvidia.yml" \
	./$(BACKEND_DIR)/scripts/compose-xg2g.sh up -d
	@echo "✅ NVIDIA stack started at http://localhost:8088"

stop-nvidia: ## Stop the NVIDIA-backed local container stack
	@XG2G_COMPOSE_ROOT="$(CURDIR)/deploy" \
	XG2G_ENV_FILE="$(CURDIR)/.env" \
	COMPOSE_FILE="docker-compose.yml:../docker-compose.dev.yml:docker-compose.nvidia.yml" \
	./$(BACKEND_DIR)/scripts/compose-xg2g.sh down
	@echo "✅ NVIDIA stack stopped"

up: ## Advanced raw Compose alias used by `make start`
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml up -d
	@echo "✅ Stack started at http://localhost:8088"

down: ## Advanced raw Compose alias used by `make stop`
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml down
	@echo "✅ Stack stopped"

status: ## Check the default local container stack on http://localhost:8088
	@curl -fsS http://localhost:8088/healthz >/dev/null 2>&1 && echo "✅ OK" || echo "❌ Service not responding"

ps: ## Show running containers for the local Compose project
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml ps

logs: ## Show logs for the local Compose project
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml logs -f

restart: ## Restart the default local Compose service (advanced)
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml restart xg2g

prod-up: ## Start production stack
	@docker compose -f deploy/docker-compose.yml up -d

prod-down: ## Stop production stack
	@docker compose -f deploy/docker-compose.yml down

release-check: ## Validate release readiness
	@$(MAKE) lint
	@$(MAKE) test-all
	@$(MAKE) verify
	@echo "✅ Release readiness verified"

changelog: ## Generate CHANGELOG.md with git-cliff
	@if command -v git-cliff >/dev/null 2>&1; then \
		git-cliff -o CHANGELOG.md; \
		echo "✅ CHANGELOG.md generated"; \
	else \
		echo "❌ git-cliff not found. Please install it to generate changelogs."; \
		exit 1; \
	fi

release: release-check ## Create and push a new release (usage: make release version=vX.Y.Z)
	@if [ -z "$(version)" ]; then \
		echo "Usage: make release version=vX.Y.Z"; \
		exit 1; \
	fi
	@echo "🚀 Creating release $(version)..."
	@git tag -a $(version) -m "Release $(version)"
	@git push origin $(version)
	@echo "✅ Tag $(version) pushed. GitHub Actions will handle the rest."

setup: build-ffmpeg ## One-time setup: build FFmpeg

build-ffmpeg: ## Build FFmpeg
	@if [ -f $(BACKEND_DIR)/scripts/build-ffmpeg.sh ]; then \
		./$(BACKEND_DIR)/scripts/build-ffmpeg.sh; \
	fi

repair-metadata: ## Remove macOS metadata
	@if [ -f $(BACKEND_DIR)/scripts/ops/repair-metadata.sh ]; then \
		bash ./$(BACKEND_DIR)/scripts/ops/repair-metadata.sh; \
	fi

runtime-sync-check: ## Compare the clean source checkout against the runtime workspace (default target: /opt/xg2g)
	@./deploy/runtime-sync.sh --check $(if $(source),--source $(source),) $(if $(target),--target $(target),) $(if $(ref),--ref $(ref),)

runtime-sync-apply: ## Sync the clean source checkout into the runtime workspace (use force=1 for a dirty target)
	@./deploy/runtime-sync.sh --apply $(if $(source),--source $(source),) $(if $(target),--target $(target),) $(if $(ref),--ref $(ref),) $(if $(filter 1,$(allow_dirty)),--allow-source-dirty,) $(if $(filter 1,$(force)),--force-target-dirty,)
