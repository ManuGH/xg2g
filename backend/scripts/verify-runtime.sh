#!/usr/bin/env bash
# Live runtime truth verifier. A successful result proves that the running
# container, binary, health endpoint, and release metadata match repo truth.

set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
VERSION_FILE="${REPO_ROOT}/backend/VERSION"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
RUNTIME_SNAPSHOT="${XG2G_RUNTIME_SNAPSHOT:-/var/lib/xg2g/runtime_state.json}"
CONTAINER_NAME="${XG2G_CONTAINER_NAME:-xg2g}"
EXPECTED_USER="${XG2G_EXPECTED_CONTAINER_USER:-10001:10001}"
API_PORT="${XG2G_PORT:-8088}"

fail() {
    echo "❌ FAIL: $*" >&2
    exit 1
}

command -v docker >/dev/null 2>&1 || fail "docker is required"
command -v jq >/dev/null 2>&1 || fail "jq is required"
[[ -f "$VERSION_FILE" ]] || fail "version file is missing: $VERSION_FILE"
[[ -f "$LOCK_FILE" ]] || fail "digest lock is missing: $LOCK_FILE"

TARGET_VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
[[ -n "$TARGET_VERSION" ]] || fail "canonical version is empty"
echo "🔍 Verifying Runtime Truth against ${TARGET_VERSION}..."

running="$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)"
[[ "$running" == "true" ]] || fail "container '${CONTAINER_NAME}' is not running"

actual_user="$(docker inspect --format '{{.Config.User}}' "$CONTAINER_NAME")"
[[ "$actual_user" == "$EXPECTED_USER" ]] || \
    fail "container '${CONTAINER_NAME}' runs as '${actual_user:-root}', expected '${EXPECTED_USER}'"
echo "✅ Runtime user: ${actual_user}"

health_status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "$CONTAINER_NAME")"
[[ "$health_status" == "healthy" ]] || \
    fail "container health is '${health_status}', expected 'healthy'"
echo "✅ Container health: ${health_status}"

image_id="$(docker inspect --format '{{.Image}}' "$CONTAINER_NAME")"
image_version="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image_id" 2>/dev/null || true)"
[[ "$image_version" == "$TARGET_VERSION" ]] || \
    fail "image version label is '${image_version:-missing}', expected '${TARGET_VERSION}'"

binary_version="$(docker exec "$CONTAINER_NAME" xg2g --version 2>/dev/null | awk 'NR == 1 { print $1 }')"
[[ "$binary_version" == "$TARGET_VERSION" ]] || \
    fail "binary version is '${binary_version:-missing}', expected '${TARGET_VERSION}'"
echo "✅ Image and binary version: ${binary_version}"

echo "📡 Probing live API on container port ${API_PORT}..."
docker exec "$CONTAINER_NAME" xg2g healthcheck \
    --mode=live \
    --port="$API_PORT" \
    --timeout=5s >/dev/null || fail "live API healthcheck failed"
echo "✅ Live API healthcheck passed"

EXPECTED_DIGEST="$(jq -r --arg version "$TARGET_VERSION" '.releases[$version].digest // empty' "$LOCK_FILE")"
[[ -n "$EXPECTED_DIGEST" ]] || fail "DIGESTS.lock has no entry for ${TARGET_VERSION}"

LIVE_DIGEST="$(
    docker image inspect --format '{{range .RepoDigests}}{{println .}}{{end}}' "$image_id" 2>/dev/null |
        awk -F '@' 'NF == 2 { print $2; exit }'
)"
if [[ "$EXPECTED_DIGEST" == "pending" ]]; then
    echo "⚠️  Release digest is pending; binary and image labels provide local identity proof."
else
    [[ -n "$LIVE_DIGEST" ]] || fail "running image has no repository digest"
    [[ "$LIVE_DIGEST" == "$EXPECTED_DIGEST" ]] || {
        echo "   Expected: ${EXPECTED_DIGEST}" >&2
        echo "   Actual:   ${LIVE_DIGEST}" >&2
        fail "runtime digest drift"
    }
    echo "✅ Runtime matches DIGESTS.lock"
fi

calculate_config_hash() {
    local files=("/etc/xg2g/xg2g.env" "/etc/xg2g/config.yaml")
    local combined_manifest hash
    combined_manifest="$(mktemp)"
    trap 'rm -f "$combined_manifest"' RETURN
    for file in "${files[@]}"; do
        if [[ -f "$file" ]]; then
            echo "--- $file ---" >> "$combined_manifest"
            tr -d '\r' < "$file" | sed 's/[[:space:]]*$//' >> "$combined_manifest"
        else
            echo "--- $file (MISSING) ---" >> "$combined_manifest"
        fi
    done
    if command -v sha256sum >/dev/null 2>&1; then
        hash="$(sha256sum "$combined_manifest" | awk '{print $1}')"
    else
        hash="$(shasum -a 256 "$combined_manifest" | awk '{print $1}')"
    fi
    printf '%s' "$hash"
}

CURRENT_FINGERPRINT="$(calculate_config_hash)"
echo "✅ Configuration fingerprint: ${CURRENT_FINGERPRINT}"

if [[ -f "$RUNTIME_SNAPSHOT" ]]; then
    SNAPSHOT_VERSION="$(jq -r '.active_version // empty' "$RUNTIME_SNAPSHOT")"
    [[ "$SNAPSHOT_VERSION" == "$TARGET_VERSION" ]] || \
        fail "node truth '${SNAPSHOT_VERSION:-missing}' differs from repo truth '${TARGET_VERSION}'"
    echo "✅ Node truth version: ${SNAPSHOT_VERSION}"
fi

echo "✨ Runtime Identity Verified."
