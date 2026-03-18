#!/bin/bash
# Best Practice 2026: Zero-Drift Digest Stability Gate
# Verifies that DIGESTS.lock is consistent with VERSION and remote registry.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
VERSION="$(cat "${REPO_ROOT}/backend/VERSION" | tr -d '[:space:]')"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
MANIFEST_FILE="${REPO_ROOT}/RELEASE_MANIFEST.json"

if [[ ! -f "$LOCK_FILE" ]]; then
    echo "❌ DIGESTS.lock missing"
    exit 1
fi

echo "🔍 Validating Digest Stability for v${VERSION}..."

# 1. Structural Validation (Basic YAML/JSON check)
# Check if VERSION exists in releases
if ! grep -q "\"${VERSION}\":" "$LOCK_FILE"; then
    echo "❌ DIGESTS.lock does not contain an entry for VERSION ${VERSION}"
    exit 1
fi

# 2. RELEASE_MANIFEST.json Consistency
if [[ -f "$MANIFEST_FILE" ]]; then
    M_VERSION=$(jq -r '.version' "$MANIFEST_FILE")
    if [[ "$M_VERSION" != "$VERSION" ]]; then
        echo "❌ RELEASE_MANIFEST.json version ($M_VERSION) differs from VERSION ($VERSION)"
        exit 1
    fi
    echo "✅ RELEASE_MANIFEST.json version matches VERSION"
fi

# 3. Context-Aware Remote Check
# Detect if we are in a trusted context (main/release in same repo)
# We assume GITHUB_ACTIONS=true and a present GITHUB_TOKEN or similar indicates trust.
IS_TRUSTED_CONTEXT=false
if [[ "${GITHUB_ACTIONS:-}" == "true" ]] && [[ -n "${GITHUB_TOKEN:-}" ]]; then
    IS_TRUSTED_CONTEXT=true
fi

# Override for local testing if needed
if [[ "${TRUSTED_RELEASE_CONTEXT:-}" == "true" ]]; then
    IS_TRUSTED_CONTEXT=true
fi

DIGEST_VAL=$(grep -A 1 "\"${VERSION}\":" "$LOCK_FILE" | grep "digest" | sed 's/.*"digest":[[:space:]]*//' | tr -d '"' | tr -d '[:space:]' | tr -d ',' | tr -d '{}')
IMAGE_REPO=$(grep "\"image\":" "$LOCK_FILE" | sed 's/.*"image":[[:space:]]*//' | tr -d '"' | tr -d '[:space:]' | tr -d ',')

if [[ -z "$DIGEST_VAL" ]]; then
    echo "❌ Could not extract digest for v${VERSION} from DIGESTS.lock"
    exit 1
fi

if [[ "$IS_TRUSTED_CONTEXT" == "true" ]]; then
    echo "🌐 Trusted context: Verifying remote existence of ${IMAGE_REPO}@${DIGEST_VAL}..."
    if ! docker manifest inspect "${IMAGE_REPO}@${DIGEST_VAL}" > /dev/null 2>&1; then
        echo "❌ FAIL: Digest ${DIGEST_VAL} not found in remote registry ${IMAGE_REPO}"
        exit 1
    fi
    echo "✅ Remote existence verified."
else
    echo "⚠️  Untrusted/Local context: Skipping remote registry check (Format-only validation)."
    if [[ "$DIGEST_VAL" == "pending" ]]; then
        echo "✅ Digest is pending (release-prepare state)."
    elif [[ ! "$DIGEST_VAL" =~ ^sha256:[a-f0-9]{64}$ ]]; then
        echo "❌ FAIL: Digest '${DIGEST_VAL}' has invalid format."
        exit 1
    else
        echo "✅ Digest format is valid."
    fi
fi

echo "✨ Digest Stability Gate Passed."
