# ===================================================================================================
# Operations and Orchestration
# ===================================================================================================

.PHONY: start stop start-gpu stop-gpu start-nvidia stop-nvidia up down status ps logs restart prod-up prod-down release-check release changelog setup build-ffmpeg repair-metadata workspace-clean-preview workspace-clean workspace-clean-aggressive

RUNTIME ?= base
DEV_COMPOSE = ./$(BACKEND_DIR)/scripts/dev-compose.sh "$(RUNTIME)"

start: doctor ## Start local containers (RUNTIME=base|vaapi|nvidia; default: base)
	@./$(BACKEND_DIR)/scripts/check-local-runtime.sh "$(RUNTIME)"
	@$(DEV_COMPOSE) up -d
	@echo "Local $(RUNTIME) stack started at http://localhost:8088"

stop: ## Stop local containers (use the same RUNTIME value as start)
	@$(DEV_COMPOSE) down

start-gpu:
	@echo "NOTE: make start-gpu is deprecated; use 'make start RUNTIME=vaapi'."
	@$(MAKE) start RUNTIME=vaapi

stop-gpu:
	@echo "NOTE: make stop-gpu is deprecated; use 'make stop RUNTIME=vaapi'."
	@$(MAKE) stop RUNTIME=vaapi

start-nvidia:
	@echo "NOTE: make start-nvidia is deprecated; use 'make start RUNTIME=nvidia'."
	@$(MAKE) start RUNTIME=nvidia

stop-nvidia:
	@echo "NOTE: make stop-nvidia is deprecated; use 'make stop RUNTIME=nvidia'."
	@$(MAKE) stop RUNTIME=nvidia

up:
	@echo "NOTE: make up is deprecated; use 'make start RUNTIME=$(RUNTIME)'."
	@$(MAKE) start RUNTIME="$(RUNTIME)"

down:
	@echo "NOTE: make down is deprecated; use 'make stop RUNTIME=$(RUNTIME)'."
	@$(MAKE) stop RUNTIME="$(RUNTIME)"

status: ## Check the default local container stack on http://localhost:8088
	@curl -fsS http://localhost:8088/healthz >/dev/null 2>&1 \
		&& echo "OK: local service is healthy" \
		|| { echo "ERROR: local service is not responding" >&2; exit 1; }

ps: ## Show running containers for the local Compose project
	@$(DEV_COMPOSE) ps

logs: ## Show logs for the local Compose project
	@$(DEV_COMPOSE) logs -f

restart: ## Restart the default local Compose service (advanced)
	@$(DEV_COMPOSE) restart xg2g

prod-up:
	@echo "ERROR: Production deployment is not a Make target." >&2
	@echo "Use: deploy/sync.sh --apply --ref <tag|sha>" >&2
	@false

prod-down:
	@echo "ERROR: Production deployment is not a Make target." >&2
	@echo "Use the installed lifecycle: systemctl stop xg2g" >&2
	@false

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

workspace-clean-preview: ## Preview safe local workspace cleanup actions
	@./$(BACKEND_DIR)/scripts/ops/cleanup-workspace.sh

workspace-clean: ## Remove safe local build/test outputs and clean auxiliary worktrees
	@./$(BACKEND_DIR)/scripts/ops/cleanup-workspace.sh --apply

workspace-clean-aggressive: ## Also remove stale /tmp/xg2g-* dirs and local bin/ outputs
	@./$(BACKEND_DIR)/scripts/ops/cleanup-workspace.sh --apply --aggressive
