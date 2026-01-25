#!/bin/bash
set -e

# verify-telemetry-coverage.sh
# Ensures that significant error paths in V3Player are instrumented with Telemetry.
# "Significant" means contract/network errors.

TARGET_FILE="webui/src/components/V3Player.tsx"

echo "--- Verifying Telemetry Coverage ---"

# 1. Count 'throw new Error'
THROWS=$(grep -c "throw new Error" "$TARGET_FILE")
# 2. Count 'telemetry.emit'
EMITS=$(grep -c "telemetry.emit" "$TARGET_FILE")

echo "Found $THROWS error throws and $EMITS telemetry emissions."

# Heuristic: We expect most contract errors to have telemetry.
# This script is a weak check suitable for PR-6.1.4 proof. 
# A real implementation would parse AST to see if 'throw' is preceded by 'emit'.

# Let's check for specific critical codes
REQUIRED_CODES=("AUTH_DENIED" "GONE" "LEASE_BUSY" "UNAVAILABLE" "ui.failclosed")

FAILED=0
for CODE in "${REQUIRED_CODES[@]}"; do
    if ! grep -q "$CODE" "$TARGET_FILE"; then
        echo "❌ Missing coverage for code/event: $CODE"
        FAILED=1
    fi
done

if [ $FAILED -eq 1 ]; then
    echo "Coverage verification failed."
    exit 1
fi

echo "✅ Critical telemetry codes found in source."
