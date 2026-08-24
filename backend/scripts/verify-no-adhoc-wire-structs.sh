#!/usr/bin/env bash
set -euo pipefail

ROOT="${REPO_ROOT:-$(pwd)}"
cd "$ROOT"

echo "--- verify-no-adhoc-wire-structs ---"
go run ./scripts/verify-no-adhoc-wire-structs.go
