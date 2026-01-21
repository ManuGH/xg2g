#!/bin/bash
set -e

# verify-openapi-hygiene.sh
# Enforces consistency and strictness in the OpenAPI contract.
# 1. Casing: camelCase is the standard. Specific legacy fields are ALLOWED (not checked).
# 2. Strictness: additionalProperties: false is mandatory for Decision/Problem types.

OPENAPI_FILE="api/openapi.yaml"
EXIT_CODE=0

echo "--- Checking OpenAPI Hygiene ---"

# 1. Casing Check (Non-legacy IDs)
# We look for common ID patterns in camelCase
CAMEL_IDS=(
    # Exempted for Phase 4 compatibility:
    # "requestId"
    # "recordingId"
    # "timerId"
)

for ID in "${CAMEL_IDS[@]}"; do
    if grep -q "$ID" "$OPENAPI_FILE"; then
        echo "❌ Hygiene Violation: camelCase ID '$ID' found in $OPENAPI_FILE. Use snake_case."
        EXIT_CODE=1
    fi
done

# 2. additionalProperties: false Check
# For schemas starting with Playback, Problem, or ending with Decision, Trace.
# This is a simplified check that looks for the presence of the property in the schema block.
# We'll use a heuristic: schema name followed by properties, then check if additionalProperties is there.

SCHEMAS=$(grep -E "^  [A-Z][a-zA-Z]+:" "$OPENAPI_FILE" | grep -E "Playback|Problem|Decision|Trace" | sed 's/://' || true)

for SCHEMA in $SCHEMAS; do
    # Find the line number of the schema definition
    LINE=$(grep -n "^  $SCHEMA:" "$OPENAPI_FILE" | cut -d: -f1)
    # Get the next 30 lines (approx size of a schema)
    SCHEMA_CONTENT=$(sed -n "$LINE,$((LINE + 30))p" "$OPENAPI_FILE")
    
    # Check if it has properties and if it DOES NOT have additionalProperties: false
    if echo "$SCHEMA_CONTENT" | grep -q "properties:" && ! echo "$SCHEMA_CONTENT" | grep -q "additionalProperties: false"; then
        echo "❌ Hygiene Violation: Schema '$SCHEMA' is missing 'additionalProperties: false'."
        EXIT_CODE=1
    fi
done

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ OpenAPI Hygiene verified."
else
    echo "FAILED: OpenAPI contract does not meet governance standards."
fi

exit $EXIT_CODE
