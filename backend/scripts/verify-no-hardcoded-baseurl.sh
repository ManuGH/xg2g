#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(pwd)}"
cd "$REPO_ROOT"

echo "--- verify-no-hardcoded-baseurl ---"

matches=$(rg -n '"/api/v3"' internal/ \
  --glob '!internal/control/http/v3/baseurl.go' || true)

if [[ -n "${matches}" ]]; then
  echo "❌ Hardcoded V3 base URL found:"
  echo "${matches}"
  exit 1
fi

echo "✅ No hardcoded V3 base URL found in internal/ (excluding baseurl.go)"
