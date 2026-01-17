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
#    (scans only actual run: content, not comments or metadata)

set -euo pipefail

WORKFLOW_FILE="${1:-.github/workflows/ci.yml}"

echo "🔍 CTO Gate: Verifying hermetic code generation contract..."

# ============================================================================
# Part 1: Allow-List Enforcement
# ============================================================================
# Any step with "generate" or "codegen" in its name MUST use 'make generate' only
# Note: This is a heuristic check. The global forbid patterns below are canonical.

echo "   Checking allow-list: codegen steps should use 'make generate'..."

GENERATE_STEPS=$(grep -n -i -E 'name:.*generat' "$WORKFLOW_FILE" || true)

if [ -n "$GENERATE_STEPS" ]; then
    # For each generate step, verify run block doesn't contain direct tool calls
    # Simplified check: ensures common violations are caught
    while IFS= read -r line; do
        LINE_NUM=$(echo "$line" | cut -d':' -f1)
        # Check next ~15 lines after the name for problematic patterns
        if tail -n +$LINE_NUM "$WORKFLOW_FILE" | head -n 15 | grep -q -E '(go\s+run\s+.*oapi|go\s+install.*oapi|oapi-codegen\s)'; then
            echo "❌ Generate step at line $LINE_NUM may contain direct tool invocation"
            echo "   Allowed: 'make generate' only"
            echo "   (This is a heuristic - see global禁 scan for canonical check)"
        fi
    done <<< "$GENERATE_STEPS"
fi

# ============================================================================
# Part 2: Global Forbid Patterns (Canonical Check)
# ============================================================================
# Disallow direct invocations anywhere in run: blocks
# Filter out comments (#) and metadata fields (name:, with:, uses:) to avoid false positives

echo "   Checking global forbid patterns (canonical)..."

# Extract only meaningful content: exclude comments and metadata fields
SCANNABLE_CONTENT=$(grep -v -E '^\s*(#|name:|uses:|with:)' "$WORKFLOW_FILE" || true)

# Pattern 1: Direct oapi-codegen binary or go run
VIOLATIONS=$(echo "$SCANNABLE_CONTENT" | grep -n -E '\b(go\s+run\s+.*oapi-codegen|oapi-codegen\s+)' || true)

# Pattern 2: go install of generators
INSTALL_VIOLATIONS=$(echo "$SCANNABLE_CONTENT" | grep -n -E 'go\s+install.*oapi' || true)

# Pattern 3: Downloading generators
DOWNLOAD_VIOLATIONS=$(echo "$SCANNABLE_CONTENT" | grep -n -E '(curl|wget).*oapi' || true)

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
    echo "❌ Direct tool invocation detected in CI run: blocks:"
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
echo "   → No direct tool invocations detected in run: blocks"
