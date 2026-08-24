#!/usr/bin/env bash
set -euo pipefail

ROOT="${REPO_ROOT:-$(pwd)}"
cd "$ROOT"

echo "--- verify-client-transport-boundary ---"
go run ./scripts/verify-client-transport-boundary.go
