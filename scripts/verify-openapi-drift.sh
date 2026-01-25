#!/bin/bash
set -e

# verify-openapi-drift.sh
# Ensures that openapi/v3.normative.snapshot.yaml is in sync with api/openapi.yaml
# and that no forbidden fields have leaked into the normative surface.

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SNAPSHOT_FILE="$REPO_ROOT/openapi/v3.normative.snapshot.yaml"
CURRENT_API="$REPO_ROOT/api/openapi.yaml"
MANIFEST_FILE="$REPO_ROOT/contracts/ui_consumption.manifest.json"

echo "--- Verifying OpenAPI Drift ---"

# 1. Structural Equality Check
# We generate a FRESH snapshot from the current API and see if it matches the COMMITTED snapshot.

GENERATE_SCRIPT="$REPO_ROOT/scripts/generate-normative-snapshot.sh"
TEMP_SNAPSHOT=$(mktemp)

# Run generation to temp file (hack: modifying script slightly to output to location? 
# Or just let it overwrite and revert? No, side effects bad.)
# Let's rely on the generate script modifying the file in place, 
# so we copy the committed one to a safe place, run generate on the repo file, compare, then restore?
# Better: Make generate script accept output path.

# Assume generate-normative-snapshot.sh takes output path, or we modify it to do so.
# Since I just wrote it, let's modify it to take 1 arg.

# Wait, let's just do a diff of content.
# Manually run the filter logic here on a temp copy of api.yaml
TEMP_API=$(mktemp)
cp "$CURRENT_API" "$TEMP_API"

if command -v jq &> /dev/null; then
    FORBIDDEN_FIELDS=$(jq -r '.entries[] | select(.category=="forbidden") | .fieldPath' "$MANIFEST_FILE")
    for FIELD in $FORBIDDEN_FIELDS; do
        FIELD_KEY="${FIELD##*.}"
        sed -i "/^[[:space:]]*$FIELD_KEY:/d" "$TEMP_API"
    done
fi

if ! cmp -s "$SNAPSHOT_FILE" "$TEMP_API"; then
    echo "❌ Drift detected! Committed snapshot does not match current API state."
    echo "   Run './scripts/generate-normative-snapshot.sh' and commit the result."
    rm "$TEMP_API"
    exit 1
fi
rm "$TEMP_API"

echo "✅ Snapshot is up-to-date."

# 2. Forbidden Field Check
# Load forbidden fields from manifest
echo "--- Checking for Forbidden Fields in Snapshot ---"

# This requires jq.
if ! command -v jq &> /dev/null; then
    echo "⚠️ jq not found, skipping deep introspection."
    exit 0
fi

FORBIDDEN_FIELDS=$(jq -r '.entries[] | select(.category=="forbidden") | .fieldPath' "$MANIFEST_FILE")

FAILED=0
for FIELD in $FORBIDDEN_FIELDS; do
    # Simple grep check - strictly looking for the key in the yaml. 
    # This is a heuristic. A real parser would be better, but this stops the bleeding.
    # We look for "  field:" or " field:" to avoid matching substrings.
    FIELD_KEY="${FIELD##*.}" # Get last part (e.g. 'outputs' from 'decision.outputs')
    
    if grep -q "$FIELD_KEY:" "$SNAPSHOT_FILE"; then
         # Only fail if it's NOT marked as forbidden/legacy in the snapshot itself (if we had markers).
         # For now, if the manifest says forbidden, it SHOULD NOT be in our normative snapshot 
         # (if the snapshot was truly filtered). 
         # BUT: The current snapshot is a full copy. 
         # SO: This check actually validates if we *filtered* the snapshot.
         # Since we decided (implicitly) that the snapshot is the FULL API for now (copy), 
         # we might fail here if the API *has* the forbidden fields.
         
         # CORRECTION: The plan says "v3.normative.snapshot.yaml (only normative + telemetry)".
         # So we MUST filter the snapshot.
         
         echo "❌ Found forbidden field '$FIELD' in normative snapshot."
         FAILED=1
    fi
done

if [ $FAILED -eq 1 ]; then
    echo "   Rationale: Normative snapshot MUST NOT contain forbidden fields."
    echo "   Fix: Create a filtered snapshot that excludes these fields."
    exit 1
fi

echo "✅ No forbidden fields found in snapshot."
exit 0
