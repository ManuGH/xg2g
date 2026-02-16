#!/bin/bash
# Best Practice 2026: Docker Image Version Consistency Gate
# Enforces that all documentation and systemd units use pinned, canonical versions.

set -euo pipefail

# 1. Single Source of Truth
# Support REPO_ROOT override for negative testing
REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
VERSION_FILE="${REPO_ROOT}/VERSION"

if [[ ! -f "$VERSION_FILE" ]]; then
    echo "❌ VERSION file not found in repo root: ${VERSION_FILE}"
    exit 1
fi

CANONICAL_VERSION=$(cat "$VERSION_FILE" | tr -d '[:space:]')
echo "🔍 Canonical Version (SSoT): ${CANONICAL_VERSION}"

# 2. Scope
FILES=(
    "${REPO_ROOT}/README.md"
    "${REPO_ROOT}/docker-compose.yml"
    "${REPO_ROOT}/docs/ops/OPERATIONS_MODEL.md"
    "${REPO_ROOT}/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md"
    "${REPO_ROOT}/docs/ops/xg2g.service"
)

# Also check all other markdown files in docs/
while IFS= read -r file; do
    FILES+=("$file")
done < <(find "${REPO_ROOT}/docs" -name "*.md")

# 3. Logic
EXIT_CODE=0
IMAGE_BASE="ghcr.io/manugh/xg2g"

# Regex: ghcr.io/manugh/xg2g[:@](tag_or_digest)
# Group 1: : or @
# Group 2: version tag or sha256 digest
REGEX="${IMAGE_BASE}([:@])([a-zA-Z0-9.-]+|sha256:[a-f0-9]{64})"

echo "🕵️ Scanning ${#FILES[@]} files for image tag drift..."

for file in "${FILES[@]}"; do
    if [[ ! -f "$file" ]]; then continue; fi
    
    # 4. Universal Forbidden Check: :latest
    if grep -q ":latest" "$file"; then
        echo "❌ FAIL: ${file} contains forbidden :latest tag"
        EXIT_CODE=1
    fi

    # 5. Drift Check (Specific to ghcr.io/manugh/xg2g)
    # Extract matches
    # Using grep -o to find all occurrences
    MATCHES=$(grep -oE "${REGEX}" "$file" || true)
    
    if [[ -z "$MATCHES" ]]; then
        continue
    fi

    while IFS= read -r match; do
        # Extract the part after : or @
        TYPE=$(echo "$match" | sed -E "s|.*${IMAGE_BASE}([:@]).*|\1|")
        VALUE=$(echo "$match" | sed -E "s|.*${IMAGE_BASE}[:@](.*)|\1|")

        if [[ "$TYPE" == ":" ]]; then
            if [[ "$VALUE" == "latest" ]]; then
                echo "❌ FAIL: ${file} contains forbidden :latest tag"
                EXIT_CODE=1
            elif [[ "$VALUE" != "$CANONICAL_VERSION" ]]; then
                echo "❌ FAIL: ${file} contains drifting tag '${VALUE}' (expected '${CANONICAL_VERSION}')"
                EXIT_CODE=1
            fi
        elif [[ "$TYPE" == "@" ]]; then
            # Digest pinning is allowed (Advanced Option)
            if [[ ! "$VALUE" =~ ^sha256:[a-f0-9]{64}$ ]]; then
                echo "❌ FAIL: ${file} contains malformed digest '${VALUE}'"
                EXIT_CODE=1
            else
                echo "✅ INFO: ${file} uses Digest pinning (Advanced)"
            fi
        fi
    done <<< "$MATCHES"
done

if [[ $EXIT_CODE -eq 0 ]]; then
    echo "✅ PASS: All image references are consistent with Best Practice 2026."
else
    echo "❌ FAIL: Version drift or forbidden tags detected."
fi

exit $EXIT_CODE
