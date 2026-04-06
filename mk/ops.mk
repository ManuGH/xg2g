# ===================================================================================================
# Operations and Orchestration
# ===================================================================================================

.PHONY: up down status ps logs restart prod-up prod-down prod-ps prod-logs prod-restart release-check release changelog setup build-ffmpeg repair-metadata

up: ## Start docker-compose stack
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml up -d
	@echo "✅ Stack started at http://localhost:8088"

down: ## Stop docker-compose stack
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml down
	@echo "✅ Stack stopped"

status: ## Check API status
	@curl -fsS http://localhost:8088/healthz >/dev/null 2>&1 && echo "✅ OK" || echo "❌ Service not responding"

ps: ## Show running containers
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml ps

logs: ## Show service logs
	@docker compose --project-directory . -f deploy/docker-compose.yml -f docker-compose.dev.yml logs -f

restart: ## Restart service
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
