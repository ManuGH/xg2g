#!/bin/bash
set -e

# ci_gate_webui_logging.sh
# Enforces WebUI logging hygiene:
# - No direct console usage outside the logging helper.
# - No token/authorization/bearer strings in log calls.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WEBUI_SRC="$ROOT_DIR/webui/src"
EXIT_CODE=0

if [ ! -d "$WEBUI_SRC" ]; then
  echo "warning: $WEBUI_SRC not found, skipping logging gate"
  exit 0
fi

echo "Running WebUI Logging Gate..."

console_matches=$(rg -n "console\\." "$WEBUI_SRC" \
  -g '!**/utils/logging.ts' \
  -g '!**/node_modules/**' \
  -g '!**/dist/**' \
  -g '!**/build/**' \
  -g '!**/coverage/**' \
  || true)
if [ -n "$console_matches" ]; then
  echo "❌ Direct console usage found outside logging helper:"
  echo "$console_matches"
  EXIT_CODE=1
fi

secret_matches=$(rg -n -i "(debugLog|debugWarn|debugError)\\([^\\)]*(token|authorization|bearer)" "$WEBUI_SRC" \
  -g '!**/node_modules/**' \
  -g '!**/dist/**' \
  -g '!**/build/**' \
  -g '!**/coverage/**' \
  || true)
if [ -n "$secret_matches" ]; then
  echo "❌ Secret-like strings found in log calls:"
  echo "$secret_matches"
  EXIT_CODE=1
fi

if [ $EXIT_CODE -eq 0 ]; then
  echo "✅ WebUI Logging Gate Passed"
else
  echo "❌ WebUI Logging Gate Failed"
fi

exit $EXIT_CODE
