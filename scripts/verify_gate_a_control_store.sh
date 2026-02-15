#!/bin/bash
# Gate A: Control Layer Store Purity Check
# ADR-014 Phase 1 - Ensures internal/control cannot directly access domain stores
#
# This script checks ONLY internal/control/** (NOT legacy internal/api)
# Legacy violations in internal/api will be addressed in PR #4/5

set -e

USE_RG=1
if ! command -v rg >/dev/null 2>&1; then
    USE_RG=0
    echo "⚠️ rg (ripgrep) not found; falling back to grep"
fi

echo "=== Gate A: Control Layer Store Purity Check ==="
echo "Scope: internal/control/** only (legacy internal/api excluded)"
echo ""

VIOLATIONS_FOUND=0

# Check 1: Import prohibition (primary check - catches 90%)
echo "[1/2] Checking for forbidden store imports in control layer..."
if [ "$USE_RG" -eq 1 ]; then
    STORE_IMPORTS=$(rg -type go --files-with-matches \
        'internal/domain/session/store' \
        internal/control/ 2>/dev/null || true)
else
    STORE_IMPORTS=$(grep -RIl --include='*.go' \
        'internal/domain/session/store' \
        internal/control/ 2>/dev/null || true)
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
if [ "$USE_RG" -eq 1 ]; then
    MUTATIONS=$(rg -type go --line-number \
        '\.(UpdateSession|PutSession|DeleteSession|TryAcquireLease|ReleaseLease)\(' \
        internal/control/ 2>/dev/null || true)
else
    MUTATIONS=$(grep -RInE --include='*.go' \
        '\.(UpdateSession|PutSession|DeleteSession|TryAcquireLease|ReleaseLease)\(' \
        internal/control/ 2>/dev/null || true)
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
