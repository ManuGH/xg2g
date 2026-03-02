#!/usr/bin/env bash
set -euo pipefail

BASE="${1:-origin/main}"

# Best-effort refresh for local usage; CI can pass an explicit base ref.
git fetch origin >/dev/null 2>&1 || true

if git diff --quiet "${BASE}...HEAD"; then
  echo "ERROR: empty diff against ${BASE}; nothing to propose"
  exit 1
fi

echo "OK: non-empty diff against ${BASE}"
