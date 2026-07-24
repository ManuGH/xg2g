#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
# Enforces Invariant I1: No hardcoded decision logic or prohibited literals outside domain media packages.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

echo "Checking for prohibited decision literals outside domain packages..."

# Forbidden codec/container literals that should only be referenced via domain constants
FORBIDDEN_PATTERNS=(
  "\"mpeg2video\""
)

VIOLATIONS=0

for pattern in "${FORBIDDEN_PATTERNS[@]}"; do
  matches=$(grep -rn "$pattern" "$REPO_ROOT/backend/internal/" \
    --exclude-dir="playbackprofile" \
    --exclude-dir="playbackplanner" \
    --exclude-dir="codec" \
    --exclude-dir="container" \
    --exclude-dir="decision" \
    --exclude-dir="testutil" \
    --exclude-dir="testfixtures" \
    --exclude="*_test.go" || true)

  if [ -n "$matches" ]; then
    echo "❌ Prohibited decision literal pattern $pattern found outside domain packages:"
    echo "$matches"
    VIOLATIONS=$((VIOLATIONS + 1))
  fi
done

if [ "$VIOLATIONS" -gt 0 ]; then
  echo "❌ Decision-literal linter failed with $VIOLATIONS violation(s)."
  exit 1
fi

echo "✅ Decision-literal linter passed: zero prohibited decision literals outside domain packages."
