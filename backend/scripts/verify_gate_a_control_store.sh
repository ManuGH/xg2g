#!/bin/bash
# Gate A: Control Layer Store Purity Check
# ADR-014 Phase 1 - Ensures internal/control cannot directly access domain stores
#
# This script checks ONLY internal/control/** (NOT legacy internal/api)
# Legacy violations in internal/api will be addressed in PR #4/5

set -e

echo "=== Gate A: Control Layer Store Purity Check ==="
echo "Scope: internal/control/** production code only (legacy internal/api excluded)"
echo ""

VIOLATIONS_FOUND=0

# Temporary legacy carve-outs within control/v3 until domain migration is complete.
# These are production files that still hold direct store integration today.
LEGACY_ALLOWLIST_REGEX='^internal/control/http/v3/(server|handlers_sessions|sessions_heartbeat)\.go$'

mapfile -t CONTROL_GO_FILES < <(
    git ls-files 'internal/control/**/*.go' \
        | grep -vE '_test\.go$' \
        | grep -vE "$LEGACY_ALLOWLIST_REGEX" \
        || true
)

# Check 1: Import prohibition (primary check - catches 90%)
echo "[1/2] Checking for forbidden store imports in control layer..."
STORE_IMPORTS=""
if [ "${#CONTROL_GO_FILES[@]}" -gt 0 ]; then
    STORE_IMPORTS=$(grep -l \
        'internal/domain/session/store' \
        "${CONTROL_GO_FILES[@]}" 2>/dev/null || true)
fi

if [ -n "$STORE_IMPORTS" ]; then
    echo "❌ GATE A FAIL: internal/control cannot import internal/domain/session/store"
    echo ""
    echo "Forbidden imports found in:"
    echo "$STORE_IMPORTS"
    echo ""
    echo "REASON: Control layer must call domain use-cases or publish events."
    echo "Direct store access bypasses domain invariants."
    VIOLATIONS_FOUND=1
fi

# Check 2: Direct store mutation calls (backup - catches creative bypasses)
echo "[2/2] Checking for direct store mutation calls..."
MUTATIONS=""
if [ "${#CONTROL_GO_FILES[@]}" -gt 0 ]; then
    MUTATIONS=$(grep -nH -E \
        '\.(UpdateSession|PutSession|DeleteSession|TryAcquireLease|ReleaseLease)\(' \
        "${CONTROL_GO_FILES[@]}" 2>/dev/null || true)
fi

if [ -n "$MUTATIONS" ]; then
    echo "❌ GATE A FAIL: internal/control cannot directly mutate stores"
    echo ""
    echo "Direct store mutations found:"
    echo "$MUTATIONS"
    echo ""
    echo "REASON: Control must use domain services (Service.Start/Stop) or events."
    VIOLATIONS_FOUND=1
fi

# Exit with failure if violations found
if [ $VIOLATIONS_FOUND -eq 1 ]; then
    echo ""
    echo "=== Gate A: FAILED ❌ ==="
    echo ""
    echo "Fix violations by:"
    echo "1. Replace store imports with domain service calls"
    echo "2. Publish domain events instead of direct store writes"
    echo "3. Move store access to domain layer"
    exit 1
fi

echo "✅ No store imports found in control layer"
echo "✅ No direct store mutations found in control layer"
echo ""
echo "=== Gate A: PASSED ✅ ==="
