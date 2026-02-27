#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Enforce P3.2: generated client modules must only be imported inside src/client-ts/.
# Product and test code must consume the wrapper/index surface instead.
PATTERN='from[[:space:]]+["'\''][^"'\'']*client-ts/(client|sdk|types)\.gen["'\'']|import\([[:space:]]*["'\''][^"'\'']*client-ts/(client|sdk|types)\.gen["'\''][[:space:]]*\)'

scan_targets=()
if [ "$#" -gt 0 ]; then
  for candidate in "$@"; do
    file="${candidate#./}"
    case "$file" in
      src/client-ts/*)
        continue
        ;;
      src/lib/clientWrapper.ts|src/lib/clientWrapper.test.ts)
        continue
        ;;
      *.ts|*.tsx|*.js|*.jsx)
        if [ -f "$file" ]; then
          scan_targets+=("$file")
        fi
        ;;
    esac
  done
fi

if [ "${#scan_targets[@]}" -gt 0 ]; then
  if command -v rg >/dev/null 2>&1; then
    if rg -n -- "$PATTERN" "${scan_targets[@]}"; then
      echo "❌ Direct client-ts/*.gen import detected outside wrapper boundary."
      echo "   Use imports from src/client-ts (index) or src/lib/clientWrapper."
      exit 1
    fi
  else
    if grep -n -E "$PATTERN" "${scan_targets[@]}"; then
      echo "❌ Direct client-ts/*.gen import detected outside wrapper boundary."
      echo "   Use imports from src/client-ts (index) or src/lib/clientWrapper."
      exit 1
    fi
  fi
else
  if command -v rg >/dev/null 2>&1; then
    if rg -n \
      --glob '*.{ts,tsx,js,jsx}' \
      --glob '!src/client-ts/**' \
      --glob '!src/lib/clientWrapper.ts' \
      --glob '!src/lib/clientWrapper.test.ts' \
      -- "$PATTERN" src tests; then
      echo "❌ Direct client-ts/*.gen import detected outside wrapper boundary."
      echo "   Use imports from src/client-ts (index) or src/lib/clientWrapper."
      exit 1
    fi
  else
    if grep -R -n -E "$PATTERN" src tests \
      --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx' \
      --exclude-dir='client-ts' \
      --exclude='clientWrapper.ts' \
      --exclude='clientWrapper.test.ts'; then
      echo "❌ Direct client-ts/*.gen import detected outside wrapper boundary."
      echo "   Use imports from src/client-ts (index) or src/lib/clientWrapper."
      exit 1
    fi
  fi
fi

echo "✅ Wrapper boundary verified (no direct generated client imports outside src/client-ts)."
