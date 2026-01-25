#!/bin/bash
# Best Practice 2026: Release PR Firewall
# Enforces that release PRs only modify authorized files to prevent drift.

set -euo pipefail

# 1. Detect Release PR Context
# Triggers: 
# - GITHUB_BASE_REF exists (is a PR)
# - GITHUB_HEAD_REF matches release/*
# - PR Title contains release: (passed via arg)
# - PR has label release (passed via arg)

PR_TITLE="${1:-}"
PR_LABELS="${2:-}"
HEAD_REF="${GITHUB_HEAD_REF:-}"

IS_RELEASE_PR=false

if [[ "$PR_TITLE" =~ ^release: ]]; then
    IS_RELEASE_PR=true
fi

if [[ "$PR_LABELS" =~ release ]]; then
    IS_RELEASE_PR=true
fi

if [[ "$HEAD_REF" =~ ^release/ ]]; then
    IS_RELEASE_PR=true
fi

if [[ "$IS_RELEASE_PR" == "false" ]]; then
    echo "ℹ️  Not a release PR. Skipping firewall check."
    exit 0
fi

echo "🛡️  Release PR Firewall: Validating diff scope..."

# 2. Authorized File List (Allowlist)
ALLOWLIST=(
    "VERSION"
    "DIGESTS.lock"
    "RELEASE_MANIFEST.json"
    "CHANGELOG.md"
    "README.md"
    "docker-compose.yml"
    "docs/ops/xg2g.service"
)

# 3. Get Diff
# In CI, we compare against the base ref
BASE_REF="${GITHUB_BASE_REF:-main}"
CHANGED_FILES=$(git diff --name-only "origin/${BASE_REF}"...HEAD)

EXIT_CODE=0

for file in $CHANGED_FILES; do
    AUTHORIZED=false
    for allowed in "${ALLOWLIST[@]}"; do
        if [[ "$file" == "$allowed" ]]; then
            AUTHORIZED=true
            break
        fi
    done
    
    if [[ "$AUTHORIZED" == "false" ]]; then
        echo "❌ FAIL: Unauthorized file change in release PR: ${file}"
        EXIT_CODE=1
    fi
done

if [[ $EXIT_CODE -eq 0 ]]; then
    echo "✅ PASS: Release PR diff scope is within allowlist."
else
    echo "❌ FAIL: Release PR contains prohibited changes. Only SSoT and generated docs are allowed."
fi

exit $EXIT_CODE
