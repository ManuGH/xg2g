#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# CTO Contract:
# - No @keyframes outside src/index.css
# - No animation usage outside src/index.css EXCEPT for statusPulse (keyframes remain centralized)

KEYFRAMES_PATTERN='@keyframes'
ANIMATION_PROP_PATTERN='animation[[:space:]]*:'

if command -v rg >/dev/null 2>&1; then
  if rg -n --glob '*.css' --glob '!index.css' -- "$KEYFRAMES_PATTERN" src; then
    echo "❌ Motion contract violation: @keyframes found outside src/index.css."
    exit 1
  fi

  animations="$(rg -n --glob '*.css' --glob '!index.css' -- "$ANIMATION_PROP_PATTERN" src || true)"
  if [[ -n "$animations" ]]; then
    non_allowed="$(printf '%s\n' "$animations" | rg -n -v -- 'statusPulse' || true)"
    if [[ -n "$non_allowed" ]]; then
      echo "❌ Motion contract violation: animation used outside src/index.css (only statusPulse is allowed)."
      echo "$non_allowed"
      exit 1
    fi
  fi
else
  if grep -R -n -E "$KEYFRAMES_PATTERN" src --include='*.css' --exclude='index.css'; then
    echo "❌ Motion contract violation: @keyframes found outside src/index.css."
    exit 1
  fi

  animations="$(grep -R -n -E "$ANIMATION_PROP_PATTERN" src --include='*.css' --exclude='index.css' || true)"
  if [[ -n "$animations" ]]; then
    non_allowed="$(printf '%s\n' "$animations" | grep -v -E 'statusPulse' || true)"
    if [[ -n "$non_allowed" ]]; then
      echo "❌ Motion contract violation: animation used outside src/index.css (only statusPulse is allowed)."
      echo "$non_allowed"
      exit 1
    fi
  fi
fi

echo "✅ Motion contract OK."
