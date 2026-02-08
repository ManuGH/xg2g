#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PATTERN='#[0-9a-fA-F]{3,8}'

if command -v rg >/dev/null 2>&1; then
  if rg -n --glob '*.{css,ts,tsx}' --glob '!index.css' "$PATTERN" src; then
    echo "❌ Hardcoded hex colors detected in webui/src. Use design tokens instead."
    exit 1
  fi
else
  if grep -R -n -E "$PATTERN" src --include='*.css' --include='*.ts' --include='*.tsx' --exclude='index.css'; then
    echo "❌ Hardcoded hex colors detected in webui/src. Use design tokens instead."
    exit 1
  fi
fi

echo "✅ No hardcoded hex colors detected."
