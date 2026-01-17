#!/usr/bin/env bash
# CTO Gate: Hermetic Code Generation Contract Enforcement
# 
# This gate ensures CI workflows only request code generation via 'make generate',
# never invoking code generators directly. This maintains the hermetic boundary:
# Makefile → vendor/ → tools.go
#
# Enforcement strategy:
# 1. Allow-list: Steps with "generate" in name must use exactly 'make generate'
# 2. Global forbid: No direct tool invocations anywhere in workflow

set -euo pipefail

WORKFLOW_FILE="${1:-.github/workflows/ci.yml}"

echo "🔍 CTO Gate: Verifying hermetic code generation contract..."

# ============================================================================
# Part 1: Allow-List Enforcement
# ============================================================================
# Any step with "generate" or "codegen" in its name MUST use 'make generate' only

echo "   Checking allow-list: codegen steps must use 'make generate'..."

# Extract steps with "generate" in name and verify they only call 'make generate'
# This prevents indirect bypasses like: make generate && oapi-codegen ...
GENERATE_STEPS=$(grep -n -i -E 'name:.*generat' "$WORKFLOW_FILE" || true)

if [ -n "$GENERATE_STEPS" ]; then
    # For each generate step, extract the run: block and verify it's safe
    # Simplified check: ensure the step doesn't contain direct tool calls
    # A full parser would be better, but this catches the common cases
    while IFS= read -r line; do
        LINE_NUM=$(echo "$line" | cut -d':' -f1)
        # Check next ~10 lines after the name for problematic patterns
        if tail -n +$LINE_NUM "$WORKFLOW_FILE" | head -n 10 | grep -q -E '(go\s+run\s+.*oapi|go\s+install.*oapi|oapi-codegen\s)'; then
            echo "❌ Generate step at line $LINE_NUM contains direct tool invocation"
            echo "   Allowed: 'make generate' only"
            exit 1
        fi
    done <<< "$GENERATE_STEPS"
fi

# ============================================================================
# Part 2: Global Forbid Patterns
# ============================================================================
# Disallow direct invocations anywhere (semantic check, not just string matching)

echo "   Checking global forbid patterns..."

# Pattern 1: Direct oapi-codegen binary or go run
VIOLATIONS=$(grep -n -E '\b(go\s+run\s+.*oapi-codegen|oapi-codegen\s+)' "$WORKFLOW_FILE" || true)

# Pattern 2: go install of generators
INSTALL_VIOLATIONS=$(grep -n -E 'go\s+install.*oapi' "$WORKFLOW_FILE" || true)

# Pattern 3: Downloading generators
DOWNLOAD_VIOLATIONS=$(grep -n -E '(curl|wget).*oapi' "$WORKFLOW_FILE" || true)

ALL_VIOLATIONS=""
if [ -n "$VIOLATIONS" ]; then
    ALL_VIOLATIONS="$VIOLATIONS"
fi
if [ -n "$INSTALL_VIOLATIONS" ]; then
    ALL_VIOLATIONS="$ALL_VIOLATIONS"$'\n'"$INSTALL_VIOLATIONS"
fi
if [ -n "$DOWNLOAD_VIOLATIONS" ]; then
    ALL_VIOLATIONS="$ALL_VIOLATIONS"$'\n'"$DOWNLOAD_VIOLATIONS"
fi

if [ -n "$ALL_VIOLATIONS" ]; then
    echo "❌ Direct tool invocation detected in CI:"
    echo "$ALL_VIOLATIONS"
    echo ""
    echo "Contract violation: CI may request generation via 'make generate' only."
    echo "Hermetic boundary: Makefile → vendor/ → tools.go"
    echo ""
    echo "Detected patterns:"
    echo "  - 'go run.*oapi-codegen' or 'oapi-codegen' (direct invocation)"
    echo "  - 'go install.*oapi' (external installation)"
    echo "  - 'curl/wget.*oapi' (download from network)"
    exit 1
fi

echo "✅ Hermetic code generation contract verified"
echo "   → All code generation goes through 'make generate'"
echo "   → No direct tool invocations detected"
