#!/usr/bin/env bash
# ==============================================================================
# verify-action-pins.sh - Best Practice 2026
# ==============================================================================
# Verifies that all 'uses:' in .github/workflows/*.yml that use SHA pins
# actually point to valid commits in the respective repositories.
# Handles sub-actions (e.g., github/codeql-action/analyze) correctly.
# ==============================================================================
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FAIL=0

echo "🔍 Auditing GitHub Action pins..."

# Find all 'uses:' lines with a SHA-like pin (40 chars)
USED_ACTIONS=$(grep -rE "uses:[[:space:]]+[^@]+@[a-f0-9]{40}" "$ROOT/.github/workflows" | \
    sed -E 's/.*uses:[[:space:]]+([^[:space:]]+).*/\1/' | sort -u)

if [[ -z "$USED_ACTIONS" ]]; then
    echo "✅ No SHA-pinned actions found."
    exit 0
fi

for action in $USED_ACTIONS; do
    REPO_REF="${action%#*}" # Strip comments
    FULL_PATH="${REPO_REF%@*}"
    SHA="${REPO_REF#*@}"
    
    # Extract owner/repo from path (e.g., github/codeql-action/analyze -> github/codeql-action)
    # Most actions follow owner/repo or owner/repo/path
    IFS='/' read -r -a PATH_PARTS <<< "$FULL_PATH"
    if [[ ${#PATH_PARTS[@]} -lt 2 ]]; then
        echo "  Checking $FULL_PATH@${SHA:0:7}... ⚠️  INVALID FORMAT"
        FAIL=1
        continue
    fi
    REPO="${PATH_PARTS[0]}/${PATH_PARTS[1]}"
    
    echo -n "  Checking $REPO@${SHA:0:7}... "
    
    # Use gh api to check if the commit exists
    if gh api "repos/$REPO/commits/$SHA" --silent 2>/dev/null; then
        echo "✅ EXISTS"
    else
        echo "❌ MISSING (Invalid SHA)"
        FAIL=1
    fi
done

if [[ "$FAIL" -ne 0 ]]; then
    echo -e "\n❌ ERROR: One or more GitHub Action pins are invalid."
    exit 1
fi

echo -e "\n✅ All GitHub Action pins are valid."
