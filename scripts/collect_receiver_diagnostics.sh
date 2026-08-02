#!/usr/bin/env bash
set -euo pipefail

# collect_receiver_diagnostics.sh
# Read-only, non-destructive diagnostic collection tool for Enigma2 / OpenWebif.
# Usage: ./scripts/collect_receiver_diagnostics.sh <OPENWEBIF_HOST_OR_IP> [SSH_USER@SSH_HOST]

RECEIVER_HOST="${1:-10.10.55.2}"
SSH_TARGET="${2:-}"
OUTPUT_DIR="$(pwd)/docs/architecture/enigma2-observations"
DATE_STAMP="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

echo "=== xg2g Read-Only Receiver Diagnostic Collector ==="
echo "Target OpenWebif: ${RECEIVER_HOST}"
echo "Output Directory: ${OUTPUT_DIR}"
echo "Observation Date: ${DATE_STAMP}"
echo ""

mkdir -p "${OUTPUT_DIR}/openwebif"
mkdir -p "${OUTPUT_DIR}/sys"

# Function to fetch and redact OpenWebif JSON endpoint
fetch_endpoint() {
    local endpoint="$1"
    local filename="$2"
    echo "[HTTP] Fetching http://${RECEIVER_HOST}${endpoint}..."
    if curl -sS --connect-timeout 5 "http://${RECEIVER_HOST}${endpoint}" > "${OUTPUT_DIR}/openwebif/${filename}.raw"; then
        # Redact potentially sensitive keys (tokens, pins, passwords)
        sed -E 's/("pin"|"password"|"token"|"auth"): *"[^"]+"/\1: "[REDACTED]"/g' "${OUTPUT_DIR}/openwebif/${filename}.raw" > "${OUTPUT_DIR}/openwebif/${filename}.json"
        rm -f "${OUTPUT_DIR}/openwebif/${filename}.raw"
        echo "  -> Saved ${filename}.json"
    else
        echo "  [WARNING] Failed to fetch http://${RECEIVER_HOST}${endpoint}"
    fi
}

echo "--- 1. Fetching OpenWebif Endpoints ---"
fetch_endpoint "/api/about" "about"
fetch_endpoint "/api/deviceinfo" "deviceinfo"
fetch_endpoint "/api/statusinfo" "statusinfo"
fetch_endpoint "/api/tunersignal" "tunersignal"
fetch_endpoint "/api/timerlist" "timerlist"
fetch_endpoint "/api/getallservices" "getallservices"
fetch_endpoint "/api/subservices" "subservices"
fetch_endpoint "/api/getcurrent" "getcurrent"

# Metadata manifest
cat << EOF > "${OUTPUT_DIR}/manifest.json"
{
  "observed_at": "${DATE_STAMP}",
  "receiver_host": "${RECEIVER_HOST}",
  "ssh_target": "${SSH_TARGET}",
  "evidence_classification": "VERIFIED_BY_RECEIVER"
}
EOF

# Fetch SSH proc/sys data if SSH target provided
if [ -n "${SSH_TARGET}" ]; then
    echo ""
    echo "--- 2. Fetching Kernel & Hardware Proc/Sys Data via SSH (${SSH_TARGET}) ---"
    ssh -o ConnectTimeout=5 "${SSH_TARGET}" "
        cat /etc/image-version 2>/dev/null || true
        echo '--- ISSUE ---'
        cat /etc/issue 2>/dev/null || true
        echo '--- UNAME ---'
        uname -a 2>/dev/null || true
    " > "${OUTPUT_DIR}/sys/receiver-identity.txt" || echo "  [WARNING] SSH identity fetch failed"

    ssh -o ConnectTimeout=5 "${SSH_TARGET}" "cat /proc/bus/nim_sockets 2>/dev/null || true" > "${OUTPUT_DIR}/sys/nim_sockets.txt" || echo "  [WARNING] SSH nim_sockets fetch failed"
    ssh -o ConnectTimeout=5 "${SSH_TARGET}" "find /sys/class/dvb -maxdepth 3 -type f 2>/dev/null | sort" > "${OUTPUT_DIR}/sys/sys_dvb_class.txt" || echo "  [WARNING] SSH sys_dvb fetch failed"
    ssh -o ConnectTimeout=5 "${SSH_TARGET}" "find /proc/stb/frontend -maxdepth 3 -type f 2>/dev/null | sort" > "${OUTPUT_DIR}/sys/proc_stb_frontend.txt" || echo "  [WARNING] SSH proc_stb fetch failed"
    ssh -o ConnectTimeout=5 "${SSH_TARGET}" "ls -la /dev/dvb/adapter* 2>/dev/null || true" > "${OUTPUT_DIR}/sys/dev_dvb_adapters.txt" || echo "  [WARNING] SSH dev_dvb fetch failed"
fi

echo ""
echo "=== Diagnostic Collection Complete ==="
echo "Raw evidence persisted in: ${OUTPUT_DIR}"
