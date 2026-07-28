#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# CTO Contract: feature-level ".btn-*" classes are forbidden.
# Use the Button primitive instead.

PATTERN='btn-[a-zA-Z0-9_-]+'

if command -v rg >/dev/null 2>&1; then
  if rg -n --glob '*.css' --glob '*.ts' --glob '*.tsx' --glob '*.js' --glob '*.jsx' -- "$PATTERN" src; then
    echo "❌ Forbidden btn-* class detected. Use the Button primitive instead."
    exit 1
  fi
else
  if grep -R -n -E "$PATTERN" src --include='*.css' --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx'; then
    echo "❌ Forbidden btn-* class detected. Use the Button primitive instead."
    exit 1
  fi
fi

echo "✅ No forbidden btn-* classes detected."
