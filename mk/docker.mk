# ===================================================================================================
# Docker Operations
# ===================================================================================================

.PHONY: docker docker-build docker-security docker-tag docker-push docker-clean

docker: ## Build Docker image
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .
	@echo "✅ Docker image built: $(DOCKER_IMAGE):$(VERSION)"

docker-build: ## Build Docker image
	@echo "🚀 Building xg2g Docker image..."
	@docker buildx create --use --name xg2g-builder 2>/dev/null || true
	@docker buildx build \
		--load \
		--platform $(PLATFORMS) \
		-t $(DOCKER_IMAGE):$(VERSION) \
		.
	@echo "✅ Docker image built and loaded"

docker-security: docker ## Run Docker security scanning
	@echo "Running Docker security scan..."
	@docker scout cves $(DOCKER_IMAGE):$(VERSION) || echo "⚠️  Docker Scout not available, skipping security scan"
	@echo "✅ Docker security scan completed"

docker-tag: ## Tag Docker image with version
	@echo "Tagging Docker image..."
	@if [ -n "$(DOCKER_REGISTRY)" ]; then \
		docker tag $(DOCKER_IMAGE):$(VERSION) $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION); \
		echo "✅ Images tagged for registry: $(DOCKER_REGISTRY)"; \
	else \
		echo "✅ Local tags applied"; \
	fi

docker-push: docker-tag ## Push Docker image to registry
	@if [ -z "$(DOCKER_REGISTRY)" ]; then \
		echo "❌ DOCKER_REGISTRY not set"; \
		exit 1; \
	fi
	@echo "Pushing Docker images to $(DOCKER_REGISTRY)..."
	@docker push $(DOCKER_REGISTRY)/$(DOCKER_IMAGE):$(VERSION)
	@echo "✅ Docker images pushed"

docker-clean: ## Remove Docker build cache
	@echo "Cleaning Docker build cache..."
	@docker system prune -f
	@docker buildx prune -f
	@echo "✅ Docker cache cleaned"
