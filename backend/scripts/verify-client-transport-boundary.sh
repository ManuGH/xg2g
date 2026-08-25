#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT/backend"

echo "--- verify-client-transport-boundary ---"
go run ./scripts/verify-client-transport-boundary.go
