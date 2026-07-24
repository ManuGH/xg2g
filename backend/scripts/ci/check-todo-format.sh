#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0
# Enforces Invariant: Naked TODO or FIXME comments without a reference (e.g. TODO(SPEC_...) or FIXME(#123)) are forbidden.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"

echo "Checking for naked TODO or FIXME comments lacking spec/issue references..."

# Find comment lines starting with // TODO or // FIXME that do not have '(' immediately following
NAKED_TODOS=$(grep -rnE '//\s*(TODO|FIXME)\b[^\(]' "$REPO_ROOT/backend/internal/" "$REPO_ROOT/backend/cmd/" \
  --exclude="*_test.go" \
  --exclude="server_gen.go" || true)

# Also check for // TODO or // FIXME at end of comment line without '('
NAKED_TODOS_EOL=$(grep -rnE '//\s*(TODO|FIXME)\s*$' "$REPO_ROOT/backend/internal/" "$REPO_ROOT/backend/cmd/" \
  --exclude="*_test.go" \
  --exclude="server_gen.go" || true)

ALL_VIOLATIONS=""
if [ -n "$NAKED_TODOS" ]; then
  ALL_VIOLATIONS="$NAKED_TODOS"
fi
if [ -n "$NAKED_TODOS_EOL" ]; then
  if [ -n "$ALL_VIOLATIONS" ]; then
    ALL_VIOLATIONS="$ALL_VIOLATIONS"$'\n'"$NAKED_TODOS_EOL"
  else
    ALL_VIOLATIONS="$NAKED_TODOS_EOL"
  fi
fi

if [ -n "$ALL_VIOLATIONS" ]; then
  echo "❌ Naked TODO/FIXME comment(s) found without spec or issue reference (must use TODO(SPEC_...) or FIXME(#...)):"
  echo "$ALL_VIOLATIONS"
  exit 1
fi

echo "✅ TODO/FIXME format check passed: zero naked TODOs found."
