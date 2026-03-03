#!/usr/bin/env bash
set -euo pipefail

FILES=(
  "internal/control/http/v3/server_gen.go"
  "internal/api/server_gen.go"
)

PATTERNS=(
  "context\\.WithValue\\(.*BearerAuthScopes"
  "options\\.ErrorHandlerFunc == nil"
  "http\\.Error\\("
)

fail=0
for f in "${FILES[@]}"; do
  if [ ! -f "$f" ]; then
    echo "❌ missing generated file: $f"
    fail=1
    continue
  fi
  for p in "${PATTERNS[@]}"; do
    if rg -n "$p" "$f" >/dev/null 2>&1; then
      echo "❌ transport-only violation in $f (pattern: $p)"
      rg -n "$p" "$f" || true
      fail=1
    fi
  done
 done

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "✅ codegen transport-only check passed"
