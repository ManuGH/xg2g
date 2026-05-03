# ===================================================================================================
# xg2g Enterprise Makefile - Dispatcher
# ===================================================================================================

# Include modular Makefile fragments
include mk/variables.mk
include mk/backend.mk
include mk/quality.mk
include mk/verify.mk
include mk/docker.mk
include mk/ops.mk

.PHONY: help
help: ## Show this help message
	@echo "xg2g - Development Makefile"
	@echo ""
	@echo "First run:"
	@echo "  make install              Bootstrap .env and WebUI dependencies"
	@echo "  make dev-tools            Install pinned developer CLI tools"
	@echo "  make doctor               Verify local toolchain and workspace"
	@echo "  make start                Start the local container stack"
	@echo "  make workspace-clean-preview"
	@echo "                            Preview safe cleanup of local build/test outputs"
	@echo ""
	@echo "Orientation:"
	@echo "  docs/dev/REPO_MAP.md      What to edit, what to generate, what to ignore"
	@echo "  Local-only output dirs    data/ logs/ artifacts/ test-results/ node_modules/ .venv/ bin/"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
