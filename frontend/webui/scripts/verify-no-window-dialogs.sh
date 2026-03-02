#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

if command -v rg >/dev/null 2>&1; then
  if rg -n --glob '*.{ts,tsx,js,jsx}' -- 'window\.confirm\s*\(' src; then
    echo "❌ window.confirm detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if rg -n --glob '*.{ts,tsx,js,jsx}' -- 'window\.alert\s*\(' src; then
    echo "❌ window.alert detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if rg -n --glob '*.{ts,tsx,js,jsx}' -- 'globalThis\.confirm\s*\(' src; then
    echo "❌ globalThis.confirm detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if rg -n --glob '*.{ts,tsx,js,jsx}' -- 'globalThis\.alert\s*\(' src; then
    echo "❌ globalThis.alert detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
else
  if grep -R -n -E 'window\.confirm[[:space:]]*\(' src --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx'; then
    echo "❌ window.confirm detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if grep -R -n -E 'window\.alert[[:space:]]*\(' src --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx'; then
    echo "❌ window.alert detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if grep -R -n -E 'globalThis\.confirm[[:space:]]*\(' src --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx'; then
    echo "❌ globalThis.confirm detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
  if grep -R -n -E 'globalThis\.alert[[:space:]]*\(' src --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx'; then
    echo "❌ globalThis.alert detected. Use UiOverlayProvider confirm/toast instead."
    exit 1
  fi
fi

echo "✅ No window.confirm/window.alert detected."
