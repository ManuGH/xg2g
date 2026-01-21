#!/bin/bash
set -e

# verify-decision-ownership.sh
# CTO-grade enforcement: Decision logic is contained within the engine.
#
# Ownership Rule (normative):
# - Only internal/control/recordings/decision/** may:
#   - Create/validate DecisionInput
#   - Call Decide(...)
#   - Generate reason codes / evaluate policies
#
# Allowed importers (exact paths):
ALLOWED_DECIDE_CALLERS=(
    "internal/control/http/v3/handlers_playback_info.go"
)

ALLOWED_DECISION_IMPORTERS=(
    "internal/control/http/v3/handlers_playback_info.go"
    "internal/control/http/v3/handlers_playback_info_test.go"
    "test/contract/p4_1/contract_matrix_test.go"
)

# Exclude from leakage checks (generated code, type definitions, tests)
LEAKAGE_EXCLUDES=(
    "server_gen.go"
    "_test.go"
    "/types/"
    "/dependencies.go"
)

EXIT_CODE=0

echo "--- Checking Decision Ownership (CTO-grade) ---"

# 1. Repo-wide scan for decision.Decide( calls
echo "  [1/3] Checking Decide() call sites..."

DECIDE_CALLS=$(git grep -n "decision\.Decide(" -- "*.go" 2>/dev/null || true)

if [ ! -z "$DECIDE_CALLS" ]; then
    while IFS= read -r line; do
        FILE_PATH=$(echo "$line" | cut -d: -f1)
        
        # Check if in decision package itself (always allowed)
        if [[ "$FILE_PATH" == internal/control/recordings/decision/* ]]; then
            continue
        fi
        
        # Check exact allowlist
        IS_ALLOWED=false
        for ALLOWED in "${ALLOWED_DECIDE_CALLERS[@]}"; do
            if [[ "$FILE_PATH" == "$ALLOWED" ]]; then
                IS_ALLOWED=true
                break
            fi
        done
        
        # Also allow test/contract/** for contract matrix
        if [[ "$FILE_PATH" == test/contract/* ]]; then
            IS_ALLOWED=true
        fi
        
        if [ "$IS_ALLOWED" = false ]; then
            echo "❌ FAIL: unauthorized Decide() call in $FILE_PATH"
            echo "   → $line"
            EXIT_CODE=1
        fi
    done <<< "$DECIDE_CALLS"
fi

# 2. Repo-wide scan for decision package imports
echo "  [2/3] Checking decision package imports..."

DECISION_IMPORTS=$(git grep -n '"github.com/ManuGH/xg2g/internal/control/recordings/decision"' -- "*.go" 2>/dev/null || true)

if [ ! -z "$DECISION_IMPORTS" ]; then
    while IFS= read -r line; do
        FILE_PATH=$(echo "$line" | cut -d: -f1)
        
        # Check if in decision package itself (always allowed)
        if [[ "$FILE_PATH" == internal/control/recordings/decision/* ]]; then
            continue
        fi
        
        # Check exact allowlist
        IS_ALLOWED=false
        for ALLOWED in "${ALLOWED_DECISION_IMPORTERS[@]}"; do
            if [[ "$FILE_PATH" == "$ALLOWED" ]]; then
                IS_ALLOWED=true
                break
            fi
        done
        
        # Also allow test/contract/** for contract matrix
        if [[ "$FILE_PATH" == test/contract/* ]]; then
            IS_ALLOWED=true
        fi
        
        if [ "$IS_ALLOWED" = false ]; then
            echo "❌ FAIL: unauthorized decision import in $FILE_PATH"
            echo "   → Only allowlisted adapters may import decision package"
            EXIT_CODE=1
        fi
    done <<< "$DECISION_IMPORTS"
fi

# 3. Handler leakage patterns (decision evaluation that should be in engine)
# NOTE: We specifically check for DECISION EVALUATION patterns, not type definitions
echo "  [3/3] Checking for decision evaluation leakage in handlers..."

HANDLER_DIR="internal/control/http/v3"

# These patterns indicate decision evaluation, not just type usage
LEAKAGE_PATTERNS=(
    'if.*AllowTranscode'
    'if.*AllowDirectStream'
    'if.*SupportsCodec'
    'switch.*mode.*direct_play'
    'switch.*mode.*transcode'
)

for PATTERN in "${LEAKAGE_PATTERNS[@]}"; do
    MATCHES=$(git grep -En "$PATTERN" -- "$HANDLER_DIR/*.go" 2>/dev/null || true)
    if [ ! -z "$MATCHES" ]; then
        # Apply exclusions
        CLEAN="$MATCHES"
        for EXCLUDE in "${LEAKAGE_EXCLUDES[@]}"; do
            CLEAN=$(echo "$CLEAN" | grep -v "$EXCLUDE" || true)
        done
        
        if [ ! -z "$CLEAN" ]; then
            echo "❌ FAIL: decision evaluation leakage in handlers:"
            echo "$CLEAN"
            EXIT_CODE=1
        fi
    fi
done

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ Decision Ownership verified (CTO-grade)."
    echo "   → Decide() calls: engine + ${#ALLOWED_DECIDE_CALLERS[@]} allowlisted adapters"
    echo "   → Decision imports: engine + ${#ALLOWED_DECISION_IMPORTERS[@]} allowlisted files"
    echo "   → No decision evaluation leakage in handlers"
else
    echo ""
    echo "FAILED: Decision ownership violation detected."
    echo "See CONTRACT_INVARIANTS.md for governance rules."
fi

exit $EXIT_CODE
