#!/bin/bash
set -e

# verify-v3-shadowing.sh
# Fails if anonymous struct shadowing or unsafe pointers casts are found in v3 handlers.

TARGET_DIR="internal/control/http/v3"

echo "--- Checking v3 Handlers for Anonymous Struct Shadowing ---"

# Search for the brittle pattern (*struct { which indicates an unsafe cast to an anonymous struct
# instead of using the canonical generated DTO types.
SHADOWS=$(grep -rnE "\(\*struct[[:space:]]*\{" "$TARGET_DIR" --exclude="*_test.go" || true)

if [ -n "$SHADOWS" ]; then
    echo "❌ Shadowing Violation: Found brittle anonymous struct casts in v3 handlers."
    echo "$SHADOWS"
    echo ""
    echo "Fix: Use the canonical generated types (e.g., *ResumeSummary) from server_gen.go."
    exit 1
else
    echo "✅ No anonymous struct shadowing found in v3 handlers."
fi

exit 0
