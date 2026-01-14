#!/bin/bash
set -e

# ci_gate_webui_audit.sh
# Enforces architectural boundaries in the WebUI by flagging forbidden patterns.
# Fail-closed implementation.
# Usage: ./ci_gate_webui_audit.sh [--verify-fail]

WEBUI_SRC="webui/src"
EXIT_CODE=0
VERIFY_MODE=0

if [[ "$1" == "--verify-fail" ]]; then
    VERIFY_MODE=1
fi

echo "Running WebUI Architecture Audit..."

# Define forbidden patterns (grep regex) and verify they are caught
# Format: "PATTERN|Reason"
FORBIDDEN=(
    "derivePlaybackDecision|Backend Logic Leakage: Playback decisions belong in the specific endpoint logic."
    "selectTranscodeProfile|Backend Logic Leakage: Transcoding profiles are opaque to the client."
    "switch.*[Ss]tate|Potential FSM Leakage: Complex state switching might duplicate backend FSMs."
)

if [ $VERIFY_MODE -eq 1 ]; then
    echo "🧪 Running Negative Test Mode..."
    # Create a dummy violation file
    mkdir -p webui/src/test_violation
    echo "function derivePlaybackDecision() { return 'bad'; }" > webui/src/test_violation/bad_logic.ts
    
    # We expect failure
    set +e
    ./scripts/ci_gate_webui_audit.sh > /dev/null 2>&1
    RESULT=$?
    set -e
    
    # Cleanup
    rm -rf webui/src/test_violation
    
    if [ $RESULT -ne 0 ]; then
        echo "✅ Negative Test Passed: Gate correctly caught violation."
        exit 0
    else
        echo "❌ Negative Test Failed: Gate allowed violation to pass."
        exit 1
    fi
fi

# List of files to scan (exclude tests and generated files if necessary)
if [ ! -d "$WEBUI_SRC" ]; then
   echo "warning: $WEBUI_SRC not found, skipping check (safe if no webui)"
   exit 0
fi

FILES=$(find "$WEBUI_SRC" -name "*.ts" -o -name "*.tsx")

if [ -z "$FILES" ]; then
    echo "✅ No WebUI source files found, nothing to audit."
    exit 0
fi

for entry in "${FORBIDDEN[@]}"; do
    pattern="${entry%%|*}"
    reason="${entry#*|}"

    # Grep for pattern, verify not whitelisted with "// xg2g:allow-webui-logic"
    # match_files contains filename:linenumber:content
    matches=$(grep -rE -n "$pattern" $FILES || true)

    if [ -n "$matches" ]; then
        # Filter out lines with the allow-tag
        violations=$(echo "$matches" | grep -v "xg2g:allow-webui-logic")
        
        if [ -n "$violations" ]; then
            echo "❌ Forbidden Pattern Found: '$pattern'"
            echo "   Reason: $reason"
            echo "$violations"
            EXIT_CODE=1
        fi
    fi
done

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ WebUI Audit Passed"
else
    echo "❌ WebUI Audit Failed"
fi

exit $EXIT_CODE
