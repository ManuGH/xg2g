#!/usr/bin/env bash
set -euo pipefail

ROOT="${REPO_ROOT:-$(pwd)}"
cd "$ROOT"

echo "--- verify-no-adhoc-session-mapping ---"
go run ./scripts/verify-no-adhoc-session-mapping.go
