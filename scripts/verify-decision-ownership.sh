#!/bin/bash
# verify-decision-ownership.sh
# CTO-grade enforcement: Decision logic is contained within the engine.
#
# Ownership Rule (normative):
# - Only internal/control/recordings/decision/** may directly use the decision package.
#
# Allowed Exceptions (minimal, auditable):
# - test/invariants/**: Invariant validation tests (tree exception).
# - test/contract/p4_1/contract_matrix_test.go: Contract matrix validation.
# - test/contract/regression_directplay_test.go: Regression test.
# - handlers_playback_info.go: Authorized production adapter (single file).

set -euo pipefail

# Deterministic scope exclusions
EXCLUDE_PATTERN="^vendor/|^node_modules/|^dist/|^build/|.*_gen\.go$|.*_generated\.go$|server_gen\.go$"

# Minimal allowlist: tree for invariants, exact files for others
ALLOWLIST_PATTERN="^test/invariants/|^test/contract/p4_1/contract_matrix_test\.go$|^test/contract/regression_directplay_test\.go$|^internal/control/http/v3/handlers_playback_info\.go$"

DECISION_PKG="github.com/ManuGH/xg2g/internal/control/recordings/decision"

HITS_TOTAL=0
HITS_EXCLUDED=0
HITS_ACTIONABLE=0
EXIT_CODE=0

echo "--- Checking Decision Ownership (Hardened Gate) ---"
echo "ALLOWLIST:"
echo "  - test/invariants/**"
echo "  - test/contract/p4_1/contract_matrix_test.go"
echo "  - test/contract/regression_directplay_test.go"
echo "  - internal/control/http/v3/handlers_playback_info.go"

# Self-check: fail-fast if allowlisted exact-files are missing (prevents silent drift)
ALLOWLIST_EXACT_FILES=(
    "test/contract/p4_1/contract_matrix_test.go"
    "test/contract/regression_directplay_test.go"
    "internal/control/http/v3/handlers_playback_info.go"
)
for f in "${ALLOWLIST_EXACT_FILES[@]}"; do
    if [ ! -f "$f" ]; then
        echo "❌ FAIL: allowlisted file missing: $f"
        echo "   → Update ALLOWLIST_PATTERN and ALLOWLIST_EXACT_FILES if file was moved/renamed."
        exit 1
    fi
done

FILES=$(git ls-files "*.go" | grep -vE "$EXCLUDE_PATTERN" || true)

if [ -z "$FILES" ]; then
    echo "No files found to scan."
    exit 0
fi

# Note: unquoted $FILES assumes no whitespace in Go filenames – accepted trade-off.
# Rationale: Go projects never use spaces; robustifying would add complexity for zero real-world benefit.
for FILE in $FILES; do
    # Skip decision engine itself
    if [[ "$FILE" == internal/control/recordings/decision/* ]]; then
        continue
    fi

    IS_EXCLUDED=false
    if echo "$FILE" | grep -qE "$ALLOWLIST_PATTERN"; then
        IS_EXCLUDED=true
    fi

    # Import Check (normative): Direct package import – fixed-string match
    IMPORT_MATCHES=$(grep -nF "\"$DECISION_PKG\"" "$FILE" 2>/dev/null || true)
    if [ -n "$IMPORT_MATCHES" ]; then
        while IFS= read -r match; do
            [ -z "$match" ] && continue
            HITS_TOTAL=$((HITS_TOTAL + 1))
            LINE=$(echo "$match" | cut -d: -f1)
            SNIPPET=$(echo "$match" | cut -d: -f2-)
            
            if [ "$IS_EXCLUDED" = true ]; then
                HITS_EXCLUDED=$((HITS_EXCLUDED + 1))
            else
                HITS_ACTIONABLE=$((HITS_ACTIONABLE + 1))
                echo "❌ IMPORT_VIOLATION: $FILE:$LINE: $SNIPPET"
                EXIT_CODE=1
            fi
        done <<< "$IMPORT_MATCHES"
    fi

    # Call Check (smell detector, best-effort): decision.Decide( pattern
    # Note: normative enforcement is the import rule; this catches aliased imports
    CALL_MATCHES=$(grep -nE "decision\.Decide\(" "$FILE" 2>/dev/null || true)
    if [ -n "$CALL_MATCHES" ]; then
        while IFS= read -r match; do
            [ -z "$match" ] && continue
            HITS_TOTAL=$((HITS_TOTAL + 1))
            LINE=$(echo "$match" | cut -d: -f1)
            SNIPPET=$(echo "$match" | cut -d: -f2-)

            if [ "$IS_EXCLUDED" = true ]; then
                HITS_EXCLUDED=$((HITS_EXCLUDED + 1))
            else
                HITS_ACTIONABLE=$((HITS_ACTIONABLE + 1))
                echo "❌ CALL_VIOLATION: $FILE:$LINE: $SNIPPET"
                EXIT_CODE=1
            fi
        done <<< "$CALL_MATCHES"
    fi
done

echo ""
echo "Summary:"
echo "  # HITS_TOTAL = matched lines (imports and calls counted independently, even within the same file)."
echo "  HITS_TOTAL=$HITS_TOTAL"
echo "  HITS_EXCLUDED=$HITS_EXCLUDED"
echo "  HITS_ACTIONABLE=$HITS_ACTIONABLE"

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ PASS: no unauthorized decision.Decide() usage detected"
else
    echo "❌ FAIL: unauthorized decision.Decide() usage detected"
fi

exit $EXIT_CODE
