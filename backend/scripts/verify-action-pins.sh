#!/usr/bin/env bash
# ==============================================================================
# verify-action-pins.sh - Best Practice 2026
# ==============================================================================
# Verifies that every remote 'uses:' ref in .github/workflows/*.yml is pinned
# to a full 40-character commit SHA. By default it also checks that each SHA
# exists in the respective remote repository.
# Handles sub-actions (e.g., github/codeql-action/analyze) correctly.
# ==============================================================================
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
FAIL=0
REMOTE_CHECK=1

if [[ "${1:-}" == "--local-only" ]]; then
    REMOTE_CHECK=0
elif [[ "$#" -gt 0 ]]; then
    echo "Usage: $0 [--local-only]" >&2
    exit 2
fi

echo "🔍 Auditing GitHub Action pins..."

UNPINNED=()
while IFS=: read -r file line rest; do
    if [[ "$rest" =~ uses:[[:space:]]*([^[:space:]#]+) ]]; then
        ref="${BASH_REMATCH[1]}"
        ref="${ref#\"}"
        ref="${ref%\"}"
        ref="${ref#\'}"
        ref="${ref%\'}"

        if [[ "$ref" == ./* || "$ref" == ../* ]]; then
            continue
        fi

        if [[ ! "$ref" =~ @[A-Fa-f0-9]{40}$ ]]; then
            UNPINNED+=("${file}:${line}: ${ref}")
        fi
    fi
done < <(grep -RInE '^[[:space:]]*-?[[:space:]]*uses:[[:space:]]*[^[:space:]#]+' "$REPO_ROOT/.github/workflows" || true)

if [[ "${#UNPINNED[@]}" -gt 0 ]]; then
    echo "❌ Remote workflow actions must use full commit SHA pins:"
    printf '  %s\n' "${UNPINNED[@]}"
    exit 1
fi

echo "✅ All remote workflow action refs are full SHA pins."

if [[ "$REMOTE_CHECK" -eq 0 ]]; then
    echo "✅ Remote SHA existence check skipped (--local-only)."
    exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
    echo "❌ gh is required for remote SHA existence checks. Use --local-only for offline validation." >&2
    exit 1
fi

# Find all 'uses:' lines with a SHA-like pin (40 chars)
USED_ACTIONS=$(grep -rE "uses:[[:space:]]+[^@]+@[A-Fa-f0-9]{40}" "$REPO_ROOT/.github/workflows" | \
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
