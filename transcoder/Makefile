.PHONY: help build run test clean docker docker-push

help: ## Show this help
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-15s %s\n", $$1, $$2}'

build: ## Build release binary
	cargo build --release

run: ## Run the transcoder
	cargo run --release

test: ## Run tests
	cargo test

clean: ## Clean build artifacts
	cargo clean

docker: ## Build Docker image
	docker build -t xg2g-transcoder:latest .

docker-push: docker ## Build and push Docker image
	docker tag xg2g-transcoder:latest ghcr.io/manugh/xg2g-transcoder:latest
	docker push ghcr.io/manugh/xg2g-transcoder:latest

fmt: ## Format code
	cargo fmt

clippy: ## Run clippy lints
	cargo clippy -- -D warnings

check: fmt clippy test ## Run all checks
