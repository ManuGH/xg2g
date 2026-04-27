#!/bin/bash
# ci_gate_repo_hygiene.sh
# Enforces zero-tolerance policy: No binaries, ZIPs, test artifacts in git tracking
# Fail-closed: Any violation = build fails

set -euo pipefail

echo "🔍 Checking repository hygiene (Zero Tolerance Policy)..."

# Check for tracked binaries, ZIPs, coverage, and local test outputs.
ARTIFACT_PATTERN='(^|/)(bin/|test-results/|playwright-report/|coverage/|daemon$|xg2g$|v3probe$|.*\.zip$|coverage\.out$|coverage\.html$|test_.*\.txt$|output\.log$|.*\.last-run\.json$)'
VIOLATIONS=""
PENDING_REMOVALS=""

while IFS= read -r path; do
    if [ -e "$path" ] || [ -L "$path" ]; then
        VIOLATIONS="${VIOLATIONS}${path}"$'\n'
        continue
    fi

    if git ls-files --deleted -- "$path" | grep -Fxq "$path"; then
        PENDING_REMOVALS="${PENDING_REMOVALS}${path}"$'\n'
        continue
    fi

    VIOLATIONS="${VIOLATIONS}${path}"$'\n'
done < <(git ls-files | grep -Ei "$ARTIFACT_PATTERN" || true)

if [ -n "$VIOLATIONS" ]; then
    echo "❌ FAIL: Artifacts tracked in git (violates Zero Tolerance):"
    printf "%s" "$VIOLATIONS"
    echo ""
    echo "💡 Fix: git rm --cached <file> && update .gitignore"
    exit 1
fi

if [ -n "$PENDING_REMOVALS" ]; then
    echo "ℹ️  Pending artifact removals detected; commit these deletions before opening a PR:"
    printf "%s" "$PENDING_REMOVALS"
fi

echo "✅ Repository Hygiene: PASS (no artifacts tracked)"
exit 0
