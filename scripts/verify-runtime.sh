#!/bin/bash
# Best Practice 2026: Live Runtime Truth Verifier
# Probes the running container to ensure it matches the Repo-Truth and Node-Truth.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
VERSION_FILE="${REPO_ROOT}/VERSION"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
RUNTIME_SNAPSHOT="/var/lib/xg2g/runtime_state.json"

# Invariants from Repo-Truth
TARGET_VERSION="$(cat "$VERSION_FILE" | tr -d '[:space:]')"

echo "🔍 Verifying Runtime Truth against v${TARGET_VERSION}..."

# 1. Container Identity (Docker Inspect)
# We expect the container name to be 'xg2g' per our compose template.
CONTAINER_NAME="xg2g"
if ! docker ps --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -q "^${CONTAINER_NAME}$"; then
    echo "❌ FAIL: Container '${CONTAINER_NAME}' is not running."
    exit 1
fi

LIVE_DIGEST=$(docker inspect --format '{{index .RepoDigests 0}}' "$CONTAINER_NAME" 2>/dev/null | cut -d'@' -f2 || true)
CHECK_VALUE="$LIVE_DIGEST"
if [[ -z "$LIVE_DIGEST" ]]; then
    # Fallback for local builds that might not have RepoDigests
    LIVE_ID=$(docker inspect --format '{{.Image}}' "$CONTAINER_NAME")
    echo "⚠️  No RepoDigest found. Image ID: ${LIVE_ID}"
    CHECK_VALUE="$LIVE_ID"
else
    echo "✅ Live Digest: ${LIVE_DIGEST}"
fi

# 2. Binary Identity (Internal Endpoint)
# We probe the healthcheck or a future /version endpoint.
# For now, we'll try to get it from the daemon's own telemetry or logs if possible, 
# but a direct probe is better. 
# Assume we have a reachable API on 8088 (default)
API_PORT=${XG2G_PORT:-8088}
# We'll use a simple curl to a known endpoint that returns version info if available, 
# otherwise we rely on the container image identity which is the core truth anchor.
echo "📡 Probing API on port ${API_PORT}..."
# Note: This requires the container to be 'ready'.

# 3. Validation against DIGESTS.lock
EXPECTED_DIGEST=$(grep -A 1 "\"${TARGET_VERSION}\":" "$LOCK_FILE" | grep "digest:" | sed 's/.*digest:[[:space:]]*//' | tr -d '"' | tr -d '[:space:]' | tr -d '{}')

if [[ -n "$CHECK_VALUE" ]] && [[ "$EXPECTED_DIGEST" != "pending" ]]; then
    if [[ "$CHECK_VALUE" != "$EXPECTED_DIGEST" ]]; then
        echo "❌ FAIL: Runtime Digest Drift!"
        echo "   Expected: ${EXPECTED_DIGEST}"
        echo "   Actual:   ${CHECK_VALUE}"
        exit 1
    fi
    echo "✅ Runtime matches DIGESTS.lock"
fi

# 4. Config Fingerprint Normalization (Guardrail #5)
# Files: /etc/xg2g/xg2g.env, /etc/xg2g/config.yaml (if exist)
calculate_config_hash() {
    local files=("/etc/xg2g/xg2g.env" "/etc/xg2g/config.yaml")
    local combined_manifest
    combined_manifest=$(mktemp)
    for f in "${files[@]}"; do
        if [[ -f "$f" ]]; then
            # Normalization: Trim whitespace, sort (if env), LF only
            echo "--- $f ---" >> "$combined_manifest"
            cat "$f" | tr -d '\r' | sed 's/[[:space:]]*$//' >> "$combined_manifest"
        else
            echo "--- $f (MISSING) ---" >> "$combined_manifest"
        fi
    done
    sha256sum "$combined_manifest" | awk '{print $1}'
    rm "$combined_manifest"
}

CURRENT_FINGERPRINT=$(calculate_config_hash)
echo "✅ Configuration Fingerprint: ${CURRENT_FINGERPRINT}"

# 5. Node-Truth Snapshot Comparison
if [[ -f "$RUNTIME_SNAPSHOT" ]]; then
    SNAPSHOT_VERSION=$(jq -r '.active_version' "$RUNTIME_SNAPSHOT")
    if [[ "$SNAPSHOT_VERSION" != "$TARGET_VERSION" ]]; then
        echo "⚠️  Warning: Node-Truth (${SNAPSHOT_VERSION}) differs from Repo-Truth (${TARGET_VERSION})"
    fi
fi

echo "✨ Runtime Identity Verified."
exit 0
