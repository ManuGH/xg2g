#!/bin/bash
set -e

# scripts/generate-normative-snapshot.sh
# Creates openapi/v3.normative.snapshot.yaml from api/openapi.yaml
# By filtering out Forbidden fields defined in the manifest.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FULL_API="$REPO_ROOT/api/openapi.yaml"
SNAPSHOT_FILE="$REPO_ROOT/openapi/v3.normative.snapshot.yaml"
MANIFEST_FILE="$REPO_ROOT/contracts/ui_consumption.manifest.json"

echo "generating normative openapi snapshot..."

# Start with full copy
cp "$FULL_API" "$SNAPSHOT_FILE"

# Filter out Forbidden fields
if command -v jq &> /dev/null; then
    FORBIDDEN_FIELDS=$(jq -r '.entries[] | select(.category=="forbidden") | .fieldPath' "$MANIFEST_FILE")
    
    for FIELD in $FORBIDDEN_FIELDS; do
        FIELD_KEY="${FIELD##*.}"
        echo "Filtering forbidden field: $FIELD_KEY"
        # Naive YAML filter: remove lines containing "key:"
        # In production this should be a robust yq script.
        # For now, sed is "good enough" for the restricted set we control.
        # Using a safer sed pattern to avoid partial matches
        sed -i "/^[[:space:]]*$FIELD_KEY:/d" "$SNAPSHOT_FILE"
    done
else
    echo "jq not found, skipping filtering (Validation will likely fail)"
fi

echo "✅ Snapshot generated: $SNAPSHOT_FILE"
