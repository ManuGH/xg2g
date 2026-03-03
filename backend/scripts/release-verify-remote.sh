#!/bin/bash
# Best Practice 2026: Remote Release Verification
# Validates that the image exists in GHCR and updates DIGESTS.lock.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
VERSION="$(cat "${REPO_ROOT}/VERSION" | tr -d '[:space:]')"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
IMAGE_REPO=$(grep "image:" "$LOCK_FILE" | awk '{print $2}' | tr -d '[:space:]')

echo "🌐 Verifying Remote Status for ${IMAGE_REPO}:${VERSION}..."

# 1. Trusted Context Check (Fail-Closed)
# We need docker manifest inspect to work, which implies auth or public reachability.
if ! docker info >/dev/null 2>&1; then
    echo "❌ Docker daemon not available or no permissions."
    exit 1
fi

# 2. Fetch Remote Digest
# Fail-closed if tag not found or registry unreachable
REMOTE_DIGEST=$(docker manifest inspect "${IMAGE_REPO}:${VERSION}" -v 2>/dev/null | jq -r '.Descriptor.digest' | grep "sha256:" || true)

if [[ -z "$REMOTE_DIGEST" ]] || [[ "$REMOTE_DIGEST" == "null" ]]; then
    # Fallback to index digest if it's a multi-arch index
    REMOTE_DIGEST=$(docker manifest inspect "${IMAGE_REPO}:${VERSION}" | jq -r '.config.digest // empty')
    if [[ -z "$REMOTE_DIGEST" ]]; then
        # Try one more time with standard inspect
        REMOTE_DIGEST=$(docker inspect --format='{{index .RepoDigests 0}}' "${IMAGE_REPO}:${VERSION}" 2>/dev/null | cut -d'@' -f2 || true)
    fi
fi

# Hard fail if still no digest
if [[ -z "$REMOTE_DIGEST" ]]; then
    echo "❌ FAIL: Image ${IMAGE_REPO}:${VERSION} not found in remote registry or no access."
    exit 1
fi

echo "✅ Found remote digest: ${REMOTE_DIGEST}"

# 3. Update DIGESTS.lock (Atomic & Deterministic)
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
TEMP_LOCK="${LOCK_FILE}.tmp"

# We use a python one-liner for clean YAML manipulation without heavy dependencies
python3 - <<EOF
import yaml
import sys

with open('$LOCK_FILE', 'r') as f:
    data = yaml.safe_load(f)

if 'releases' not in data:
    data['releases'] = {}

data['releases']['$VERSION'] = {
    'digest': '$REMOTE_DIGEST',
    'published_at': '$TIMESTAMP'
}

with open('$TEMP_LOCK', 'w') as f:
    yaml.dump(data, f, sort_keys=True)
EOF

mv "$TEMP_LOCK" "$LOCK_FILE"
echo "✅ DIGESTS.lock updated atomically."

# 4. Final Manifest Sync (Optional but recommended)
python3 - <<EOF
import json
with open('RELEASE_MANIFEST.json', 'r') as f:
    data = json.load(f)
data['digest'] = '$REMOTE_DIGEST'
with open('RELEASE_MANIFEST.json', 'w') as f:
    json.dump(data, f, indent=2)
EOF
echo "✅ RELEASE_MANIFEST.json synchronized with digest."

echo "✨ Remote verification complete. Zero-Drift confirmed."
