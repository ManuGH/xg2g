#!/bin/bash
set -e

# ci_gate_webui_logging.sh
# Enforces WebUI logging hygiene:
# - No direct console usage outside the logging helper.
# - No token/authorization/bearer strings in log calls.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WEBUI_SRC="$REPO_ROOT/frontend/webui/src"
EXIT_CODE=0

if [ ! -d "$WEBUI_SRC" ]; then
  echo "warning: $WEBUI_SRC not found, skipping logging gate"
  exit 0
fi

echo "Running WebUI Logging Gate..."

# ripgrep is not installed on GitHub-hosted runners. This gate used to call `rg`
# unconditionally with `|| true`, which swallowed the resulting "command not found"
# and left the match variable empty — so every CI run printed
#   ../../backend/scripts/ci_gate_webui_logging.sh: line 26: rg: command not found
#   ✅ WebUI Logging Gate Passed
# and verified nothing at all for as long as it existed. Prefer rg, fall back to
# grep, and refuse to report success when neither is available: a gate that cannot
# search must fail, never pass.
if command -v rg >/dev/null 2>&1; then
  scan_console() {
    rg -n "console\\." "$WEBUI_SRC" \
      -g '!**/utils/logging.ts' \
      -g '!**/node_modules/**' \
      -g '!**/dist/**' \
      -g '!**/build/**' \
      -g '!**/coverage/**' \
      || true
  }
  scan_secrets() {
    rg -n -i "(debugLog|debugWarn|debugError)\\([^\\)]*(token|authorization|bearer)" "$WEBUI_SRC" \
      -g '!**/node_modules/**' \
      -g '!**/dist/**' \
      -g '!**/build/**' \
      -g '!**/coverage/**' \
      || true
  }
elif command -v grep >/dev/null 2>&1; then
  # Mirrors the rg invocation: same excludes, restricted to the source file types rg
  # would have searched here. Verified to produce the same findings on this tree.
  GREP_EXCLUDES=(
    --exclude-dir=node_modules --exclude-dir=dist
    --exclude-dir=build --exclude-dir=coverage
  )
  scan_console() {
    grep -rnE "console\\." "$WEBUI_SRC" \
      --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx' \
      --exclude='logging.ts' "${GREP_EXCLUDES[@]}" \
      || true
  }
  scan_secrets() {
    grep -rniE "(debugLog|debugWarn|debugError)\\([^\\)]*(token|authorization|bearer)" "$WEBUI_SRC" \
      --include='*.ts' --include='*.tsx' --include='*.js' --include='*.jsx' \
      "${GREP_EXCLUDES[@]}" \
      || true
  }
else
  echo "❌ WebUI Logging Gate cannot run: neither rg nor grep is available"
  exit 1
fi

console_matches=$(scan_console)
if [ -n "$console_matches" ]; then
  echo "❌ Direct console usage found outside logging helper:"
  echo "$console_matches"
  EXIT_CODE=1
fi

secret_matches=$(scan_secrets)
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
