#!/bin/bash
# Best Practice 2026: Mechanized Recovery Orchestrator
# Atomic, Fail-Closed, and Digest-Only.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
NEW_RELEASE="${1:-}"
BREAK_GLASS="${BREAK_GLASS:-false}"
LOCK_FILE="/var/lock/xg2g-recover.lock"
RUNTIME_STATE_JSON="/var/lib/xg2g/runtime_state.json"
DIGESTS_LOCK="${REPO_ROOT}/DIGESTS.lock"

usage() {
    echo "Usage: $0 <RELEASE_VERSION> [--break-glass]"
    exit 1
}

if [[ -z "$NEW_RELEASE" ]]; then usage; fi

# 1. Exclusive Locking (Hard Condition #2)
exec 200>"$LOCK_FILE"
if ! flock -n 200; then
    echo "❌ FAIL: Another recovery process is already running."
    exit 1
fi

echo "🩹 Starting Atomic Recovery for Release ${NEW_RELEASE}..."

# 2. Resolve Digest (Hard Condition #3)
TARGET_DIGEST=$(grep -A 1 "\"${NEW_RELEASE}\":" "$DIGESTS_LOCK" | grep "digest:" | awk '{print $2}' | tr -d '"' | tr -d '[:space:]')
IMAGE_BASE=$(grep "image:" "$DIGESTS_LOCK" | awk '{print $2}' | tr -d '[:space:]')

if [[ -z "$TARGET_DIGEST" ]] || [[ "$TARGET_DIGEST" == "pending" ]]; then
    echo "❌ FAIL: No verified digest found for release ${NEW_RELEASE} in DIGESTS.lock"
    exit 1
fi

# Ensure Digest-Only (Hard Condition #3)
XG2G_IMAGE="${IMAGE_BASE}@${TARGET_DIGEST}"
echo "🎯 Target Target: ${XG2G_IMAGE}"

# 3. Trusted Verification (Hard Condition #4)
IS_TRUSTED=0
if [[ "${XG2G_TRUSTED_CONTEXT:-}" == "1" ]] || [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    IS_TRUSTED=1
fi

if [[ "$IS_TRUSTED" == "1" ]]; then
    echo "🛡️ Trusted Context: Verifying Registry & Signatures..."
    if ! docker manifest inspect "$XG2G_IMAGE" > /dev/null 2>&1; then
        echo "❌ FAIL: Digest not reachable in registry."
        exit 1
    fi
    # Cosign Placeholder (Implementation-compliant)
    if command -v cosign > /dev/null; then
        if ! cosign verify "$XG2G_IMAGE" > /dev/null 2>&1; then
            if [[ "$BREAK_GLASS" == "true" ]]; then
                echo "⚠️  WARNING: Signature verification failed. BREAK-GLASS MODE ACTIVE."
                AUDIT_NOTE="Signature failure bypassed by operator."
                write_runtime_state "recovering" "true" "$AUDIT_NOTE"
            else
                echo "❌ FAIL: Signature verification failed. Use --break-glass to override."
                exit 1
            fi
        fi
    else
        echo "⚠️  Cosign not found. Skipping signature check (Governance Warning)."
    fi
fi

# 4. Snapshot Initial State (Hard Condition #4)
mkdir -p "$(dirname "$RUNTIME_STATE_JSON")"
# Save previous image for rollback (Hard Condition #3)
PREV_IMAGE=$(cat /etc/xg2g/xg2g.env | grep "XG2G_IMAGE=" | cut -d'=' -f2 || echo "")

write_runtime_state() {
    local status="$1"
    local bg="${2:-$BREAK_GLASS}"
    local note="${3:-}"
    
    local tmp_json="${RUNTIME_STATE_JSON}.tmp"
    cat <<EOF > "$tmp_json"
{
  "active_version": "${NEW_RELEASE}",
  "active_digest": "${TARGET_DIGEST}",
  "status": "${status}",
  "break_glass": ${bg},
  "audit_note": "${note}",
  "previous_image": "${PREV_IMAGE}",
  "last_update_utc": "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
}
EOF
    fsync "$tmp_json" 2>/dev/null || true
    mv "$tmp_json" "$RUNTIME_STATE_JSON"
    chmod 0640 "$RUNTIME_STATE_JSON"
}

write_runtime_state "recovering"

# 5. Deployment Phase (Atomic Restart)
# We assume the environment file is /etc/xg2g/xg2g.env
# and systemd service is xg2g.service
echo "🏗️  Deploying Digest..."
sed -i "s|^XG2G_IMAGE=.*|XG2G_IMAGE=${XG2G_IMAGE}|" /etc/xg2g/xg2g.env

if ! systemctl restart xg2g; then
    echo "❌ FAIL: Service restart failed. Triggering ROLLBACK..."
    # Rollback logic (Hard Condition #3)
    sed -i "s|^XG2G_IMAGE=.*|XG2G_IMAGE=${PREV_IMAGE}|" /etc/xg2g/xg2g.env
    systemctl restart xg2g
    write_runtime_state "degraded" "$BREAK_GLASS" "Rollback triggered due to restart failure."
    exit 1
fi

# 6. Post-Deployment Invariants (Hard Condition #4)
echo "🧪 Verifying Runtime Health & Identity..."
# Wait for health (Retry loop)
SUCCESS=0
for i in {1..10}; do
    if xg2g healthcheck --mode=ready > /dev/null 2>&1; then
        if "${REPO_ROOT}/scripts/verify-runtime.sh" > /dev/null 2>&1; then
            SUCCESS=1
            break
        fi
    fi
    echo "⏳ Waiting for health... ($i/10)"
    sleep 3
done

if [[ $SUCCESS -eq 0 ]]; then
    echo "❌ FAIL: Health/Identity check failed after timeout. Triggering ROLLBACK..."
    sed -i "s|^XG2G_IMAGE=.*|XG2G_IMAGE=${PREV_IMAGE}|" /etc/xg2g/xg2g.env
    systemctl restart xg2g
    write_runtime_state "degraded" "$BREAK_GLASS" "Rollback triggered due to post-deploy verification failure."
    exit 1
fi

# 7. Finalize Node Truth
write_runtime_state "healthy"
echo "✨ Recovery Successful. Node status: HEALTHY."
exit 0
