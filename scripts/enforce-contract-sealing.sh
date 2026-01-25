#!/bin/bash
# enforce-contract-sealing.sh
# Mechanically enforces SPEC_UI_CONSUMPTION.md invariants.

EXIT_CODE=0

echo "--- Checking for Forbidden Field Consumption (outputs[]) ---"
# V3Player is allowed to CHECK for outputs existence but not read from it.
# We grep for indexed access or map/find operations on outputs.
VIOLATIONS=$(grep -rE "\.outputs\[|\[['\"]outputs['\"|\]\.outputs\.(map|find|filter|forEach)" webui/src --exclude-dir=node_modules)

if [ ! -z "$VIOLATIONS" ]; then
    echo "FAILED: Forbidden consumption of 'outputs[]' detected."
    echo "$VIOLATIONS"
    EXIT_CODE=1
else
    echo "PASSED: No 'outputs[]' consumption detected."
fi

echo ""
echo "--- Checking for Forbidden Field Consumption (profiles[]) ---"
VIOLATIONS=$(grep -rE "\.profiles|\[['\"]profiles['\"]\]" webui/src --exclude-dir=node_modules)

if [ ! -z "$VIOLATIONS" ]; then
    echo "FAILED: Forbidden consumption of 'profiles[]' detected (Obsolete)."
    echo "$VIOLATIONS"
    EXIT_CODE=1
else
    echo "PASSED: No 'profiles[]' consumption detected."
fi

echo ""
echo "--- Checking for Forbidden Field Consumption (transcodeParams) ---"
VIOLATIONS=$(grep -rE "\.transcodeParams|\[['\"]transcodeParams['\"]\]" webui/src --exclude-dir=node_modules)

if [ ! -z "$VIOLATIONS" ]; then
    echo "FAILED: Forbidden consumption of 'transcodeParams' detected (Backend-only)."
    echo "$VIOLATIONS"
    EXIT_CODE=1
else
    echo "PASSED: No 'transcodeParams' consumption detected."
fi

exit $EXIT_CODE
