#!/bin/bash
set -e

# generate-golden-baseline.sh
# Generates or updates the GOVERNANCE_BASELINE.json manifest.
# Requirement: jq is installed.

MANIFEST="testdata/contract/GOVERNANCE_BASELINE.json"
GOLDENS=$(find testdata/contract -name "*.expected.json" | sort)

echo "--- Generating Golden Baseline Manifest ---"

# Create a temporary JSON object
echo "{}" > "$MANIFEST"

for FILE in $GOLDENS; do
    HASH=$(sha256sum "$FILE" | cut -d' ' -f1)
    # Use jq to update the manifest
    # We use --arg to safely pass raw strings
    TMP=$(mktemp)
    jq --arg f "$FILE" --arg h "$HASH" '.[$f] = $h' "$MANIFEST" > "$TMP"
    mv "$TMP" "$MANIFEST"
done

echo "✅ Baseline manifest generated at $MANIFEST"
echo "   Count: $(jq 'keys | length' "$MANIFEST") goldens locked."
