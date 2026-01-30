#!/usr/bin/env bash
set -euo pipefail

ROOT=${REPO_ROOT:-"$(pwd)"}
SCAN_DIRS_DEFAULT="internal/domain/session/manager internal/control internal/engine"
SCAN_DIRS=${DETERMINISM_SCAN_DIRS:-"$SCAN_DIRS_DEFAULT"}
ALLOWLIST=${DETERMINISM_ALLOWLIST:-"$ROOT/determinism_allowlist.txt"}

PATTERN='time\.Sleep\(|\bEventually\(|time\.After\('

matches=""
for dir in $SCAN_DIRS; do
  if [ -d "$ROOT/$dir" ]; then
    out=$(rg -n --glob '*_test.go' "$PATTERN" "$ROOT/$dir" || true)
    if [ -n "$out" ]; then
      matches+="$out"$'\n'
    fi
  fi
done

if [ -n "$matches" ]; then
  if [ -f "$ALLOWLIST" ]; then
    filtered=$(printf "%s" "$matches" | grep -v -Ff "$ALLOWLIST" || true)
  else
    filtered="$matches"
  fi

  if [ -n "$filtered" ]; then
    echo "❌ determinism gate failed (remove Sleep/Eventually/time.After or allowlist):"
    echo "$filtered"
    exit 1
  fi
fi

echo "✅ determinism gate passed"
