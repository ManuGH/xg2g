#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

echo "--- gen-openapi-hard-mode ---"

echo "Generating Go OpenAPI artifacts..."
make generate

echo "Generating normative OpenAPI snapshot..."
./scripts/generate-normative-snapshot.sh

if [ ! -d "$REPO_ROOT/webui/node_modules" ]; then
  echo "Installing webui dependencies (npm ci)..."
  npm --prefix webui ci
fi

echo "Generating TypeScript client from OpenAPI..."
npm --prefix webui run generate-client

echo "✅ OpenAPI hard-mode generation complete"
