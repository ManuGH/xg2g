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
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
