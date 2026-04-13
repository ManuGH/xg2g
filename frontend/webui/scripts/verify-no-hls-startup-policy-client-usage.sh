#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

PATTERN='\b(startupMode|startupHeadroomSec|startupReasons)\b'
ALLOW_TAG='xg2g:allow-startup-policy-debug'

if ! command -v rg >/dev/null 2>&1; then
  echo "❌ rg is required for verify-no-hls-startup-policy-client-usage.sh"
  exit 1
fi

mapfile -t scan_targets < <(
  find src \
    \( -path 'src/client-ts' -o -path 'src/types/api' \) -prune -o \
    -type f \
    \( -name '*.ts' -o -name '*.tsx' -o -name '*.js' -o -name '*.jsx' \) \
    ! -name '*.d.ts' \
    -print | LC_ALL=C sort
)

if [ "${#scan_targets[@]}" -eq 0 ]; then
  echo "✅ No product WebUI source files found for startup policy audit."
  exit 0
fi

matches="$(rg -n -- "$PATTERN" "${scan_targets[@]}" || true)"

if [ -z "$matches" ]; then
  echo "✅ HLS startup policy debug fields are not used in product WebUI code."
  exit 0
fi

violations="$(printf '%s\n' "$matches" | grep -v "$ALLOW_TAG" || true)"

if [ -n "$violations" ]; then
  echo "❌ HLS startup policy debug fields leaked into product WebUI code."
  echo "   These fields are operator-only and must not drive client playback policy."
  echo "   Allowed usage requires the explicit marker: $ALLOW_TAG"
  printf '%s\n' "$violations"
  exit 1
fi

echo "✅ HLS startup policy debug field usage is explicitly allowlisted."
