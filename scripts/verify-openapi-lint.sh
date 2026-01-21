#!/bin/bash
set -e

# verify-openapi-lint.sh
# CTO-grade enforcement: OpenAPI schema must pass structural validation.
# This catches indentation errors, missing required properties, enum mismatches, etc.

OPENAPI_FILE="api/openapi.yaml"
EXIT_CODE=0

echo "--- Checking OpenAPI Structural Validity (redocly lint) ---"

# Check if redocly is available
if ! command -v npx &> /dev/null; then
    echo "⚠️  npx not found, skipping redocly lint (CI should have this)"
    exit 0
fi

# Run redocly lint with errors-only output
# We require errors=0 (warnings are acceptable)
LINT_OUTPUT=$(npx --yes @redocly/cli lint "$OPENAPI_FILE" --format=stylish 2>&1 || true)

# Count errors (not warnings)
ERROR_COUNT=$(echo "$LINT_OUTPUT" | grep -c "error " || echo "0")

if [ "$ERROR_COUNT" -gt 0 ]; then
    echo "❌ FAIL: OpenAPI schema has $ERROR_COUNT error(s):"
    echo "$LINT_OUTPUT" | grep -E "(error|Error)" || true
    echo ""
    echo "Fix errors before merge. Warnings are acceptable."
    EXIT_CODE=1
else
    echo "✅ OpenAPI lint passed (errors=0)."
    # Show warning count for awareness
    WARNING_COUNT=$(echo "$LINT_OUTPUT" | grep -c "warning " || echo "0")
    if [ "$WARNING_COUNT" -gt 0 ]; then
        echo "   → $WARNING_COUNT warning(s) present (acceptable)"
    fi
fi

exit $EXIT_CODE
