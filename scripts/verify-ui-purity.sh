#!/bin/bash
set -e

# verify-ui-purity.sh
# CTO-grade enforcement: "Pure Viewport" in WebUI.
#
# Rule (normative):
# - In webui/src/** NEVER access decision.outputs directly
# - ONLY allowed: selected_output_url, selected_output_kind, decision.mode
#
# Exemption Policy:
# - Exemptions require // xg2g:allow-outputs-read <ticket> comment
# - Exempted files must be listed below (allowlist-only)
# - New exemptions require ADR or PR review justification

WEBUI_DIR="webui/src"
EXIT_CODE=0

# Explicit exemption allowlist (CTO requirement)
# Only these files may contain transport/imperative logic
EXEMPT_FILES=(
    # Currently: none required - Pure Viewport achieved
)

echo "--- Checking UI Purity (Pure Viewport Enforcement) ---"

# 1. Search for forbidden access patterns (repo-wide in webui)
FORBIDDEN_PATTERNS=(
    '\.outputs\['
    '\.outputs\.at'
    '\.outputs\.length'
    '\.outputs\.map'
    '\.outputs\.filter'
    '\.outputs\.find'
    'decision\.outputs'
)

for PATTERN in "${FORBIDDEN_PATTERNS[@]}"; do
    # Use git grep for consistency with other gates
    MATCHES=$(git grep -n "$PATTERN" -- "$WEBUI_DIR/**/*.ts" "$WEBUI_DIR/**/*.tsx" 2>/dev/null || true)
    
    if [ ! -z "$MATCHES" ]; then
        while IFS= read -r line; do
            FILE_PATH=$(echo "$line" | cut -d: -f1)
            
            # Check if file is in exemption allowlist
            IS_EXEMPT=false
            for EXEMPT in "${EXEMPT_FILES[@]}"; do
                if [[ "$FILE_PATH" == "$EXEMPT" ]]; then
                    IS_EXEMPT=true
                    break
                fi
            done
            
            # Check for inline exemption comment
            LINE_CONTENT=$(echo "$line" | cut -d: -f3-)
            if echo "$LINE_CONTENT" | grep -q "xg2g:allow-outputs-read"; then
                IS_EXEMPT=true
            fi
            
            if [ "$IS_EXEMPT" = false ]; then
                echo "❌ FAIL: direct outputs[] access in $FILE_PATH"
                echo "   → Pattern: $PATTERN"
                echo "   → $line"
                EXIT_CODE=1
            fi
        done <<< "$MATCHES"
    fi
done

# 2. Count inline exemptions (must be 0 in main branch)
EXEMPTION_COUNT=$(git grep -c "xg2g:allow-outputs-read" -- "$WEBUI_DIR" 2>/dev/null || echo "0")
if [ "$EXEMPTION_COUNT" -gt 0 ]; then
    echo "⚠️  Warning: $EXEMPTION_COUNT inline exemption(s) found"
    echo "   → Exemptions require ADR or PR justification"
fi

if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ UI Purity verified (Pure Viewport)."
    echo "   → No direct outputs[] access"
    echo "   → Exemption allowlist: ${#EXEMPT_FILES[@]} files"
    echo "   → Inline exemptions: $EXEMPTION_COUNT"
else
    echo ""
    echo "FAILED: UI must use selected_output_* instead of outputs[]"
    echo "See CONTRACT_INVARIANTS.md for governance rules."
fi

exit $EXIT_CODE
