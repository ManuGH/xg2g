#!/bin/bash
set -e

# verify-ui-consumption-vs-openapi.sh
# Ensures that normative consumption in the UI is backed by the normative OpenAPI snapshot.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SNAPSHOT_FILE="$REPO_ROOT/openapi/v3.normative.snapshot.yaml"
MANIFEST_FILE="$REPO_ROOT/contracts/ui_consumption.manifest.json"

echo "--- Verifying UI Consumption vs OpenAPI Snapshot ---"

if [ ! -f "$SNAPSHOT_FILE" ]; then
    echo "❌ Normative snapshot not found. Run verify-openapi-drift.sh first."
    exit 1
fi

if ! command -v jq &> /dev/null; then
  echo "⚠️ jq not found, skipping deep check."
  exit 0
fi

# 1. Validate that all normative fields in the Manifest actually exist in the Snapshot
NORMATIVE_FIELDS=$(jq -r '.entries[] | select(.category=="normative") | .fieldPath' "$MANIFEST_FILE")

FAILED=0

for FIELD in $NORMATIVE_FIELDS; do
  FIELD_KEY="${FIELD##*.}"
  # Simple grep check against snapshot
  if ! grep -q "$FIELD_KEY:" "$SNAPSHOT_FILE"; then
    echo "❌ Manifest violation: Normative field '$FIELD' is NOT present in the normative OpenAPI snapshot."
    FAILED=1
  fi
done

if [ $FAILED -eq 1 ]; then
  echo "   Rationale: You cannot mark a field as 'normative' if it is not in the sealed API contract."
  exit 1
fi

echo "✅ All normative manifest entries exist in the contract snapshot."

# 2. (Optional/Heavy) Grep codebase for fields that are NOT in the snapshot
# This is hard with grep, aiming for AST scan in PR-6.0.3.
# For now, we trust the manifest + allowed fields check.

exit 0
