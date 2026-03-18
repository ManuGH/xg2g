#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKEND_ROOT="$REPO_ROOT/backend"
cd "$REPO_ROOT"

echo "--- gen-openapi-hard-mode ---"

echo "Generating Go OpenAPI artifacts..."
make generate

echo "Generating normative OpenAPI snapshot..."
"$BACKEND_ROOT/scripts/generate-normative-snapshot.sh"

echo "Generating consumption contract types..."
node "$BACKEND_ROOT/scripts/generate-consumption-types.mjs"

if [ ! -x "$REPO_ROOT/frontend/webui/node_modules/.bin/openapi-ts" ]; then
  echo "Installing webui dependencies (npm ci)..."
  npm --prefix frontend/webui ci
fi

echo "Generating TypeScript client from OpenAPI..."
npm --prefix frontend/webui run generate-client

echo "✅ OpenAPI hard-mode generation complete"
