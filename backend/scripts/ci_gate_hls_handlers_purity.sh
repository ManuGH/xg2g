#!/bin/bash
set -e

HANDLER_FILE="internal/control/http/v3/handlers_hls.go"

echo "Checking purity of $HANDLER_FILE..."

# 1. Forbid forbidden imports
# Allow: net/http, errors, artifacts, helpers
# Forbid: os, path, vod, recordings (except helpers?)

# We grep for lines starting with strict import OR usage?
# Go imports can be multiline.
# Let's simple grep for forbidden strings.

FORBIDDEN=(
  "\"os\""
  "\"path/filepath\""
  "\"github.com/ManuGH/xg2g/internal/control/vod\""
  "vodManager."        # No manager access
  "RecordingCacheDir"  # No caching logic
  "RewritePlaylistType" # No rewrite logic
)

FAILED=0

for term in "${FORBIDDEN[@]}"; do
  if grep -Fq "$term" "$HANDLER_FILE"; then
    echo "[FAIL] Found forbidden term '$term' in $HANDLER_FILE"
    FAILED=1
  fi
done

if [ $FAILED -eq 1 ]; then
  echo "Handler purity check FAILED. Handlers must be pure adapters."
  exit 1
fi

echo "Handler purity check PASSED."
