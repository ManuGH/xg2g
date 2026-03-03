#!/bin/bash
# ci_gate_repo_hygiene.sh
# Enforces zero-tolerance policy: No binaries, ZIPs, test artifacts in git tracking
# Fail-closed: Any violation = build fails

set -e

echo "🔍 Checking repository hygiene (Zero Tolerance Policy)..."

# Check for tracked binaries, ZIPs, coverage, test outputs
VIOLATIONS=$(git ls-files | grep -Ei '(^|/)(bin/|daemon$|xg2g$|v3probe$|.*\.zip$|coverage\.out$|coverage\.html$|test_.*\.txt$|output\.log$)' || true)

if [ -n "$VIOLATIONS" ]; then
    echo "❌ FAIL: Artifacts tracked in git (violates Zero Tolerance):"
    echo "$VIOLATIONS"
    echo ""
    echo "💡 Fix: git rm --cached <file> && update .gitignore"
    exit 1
fi

echo "✅ Repository Hygiene: PASS (no artifacts tracked)"
exit 0
