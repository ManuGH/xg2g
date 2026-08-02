#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0

set -euo pipefail
umask 077

# Passive Diagnostic Collector (Phase 1)
# Purely read-only diagnostic collection for Enigma2 / OpenWebif.
# Strictly FORBIDDEN from performing zapping, streaming, timer creation,
# recording start, config mutation, or Enigma2 restarts.

COLLECTOR_VERSION="1.0.0"

# Usage check: Target MUST be passed explicitly. No default IP allowed.
if [ -z "${1:-}" ]; then
    echo "ERROR: Target OpenWebif URL or host must be specified explicitly!"
    echo "Usage: $0 <OPENWEBIF_HOST_OR_URL> [SSH_TARGET]"
    echo "Examples:"
    echo "  $0 192.168.1.100"
    echo "  $0 http://192.168.1.100:80"
    echo "  $0 192.168.1.100 root@192.168.1.100"
    exit 1
fi

RAW_TARGET="$1"
SSH_TARGET="${2:-}"
TIMESTAMP="$(date -u +"%Y%m%dT%H%M%SZ")"

# Normalize Target URL / Host
TARGET_URL="${RAW_TARGET}"
if [[ "${TARGET_URL}" != http://* ]] && [[ "${TARGET_URL}" != https://* ]]; then
    TARGET_URL="http://${RAW_TARGET}"
fi
# Strip trailing slashes
TARGET_URL="${TARGET_URL%/}"

# Base output directory - ALWAYS in gitignored var/diagnostics/
BASE_DIR="$(pwd)/var/diagnostics/enigma2/${TIMESTAMP}"
OPENWEBIF_DIR="${BASE_DIR}/openwebif"
SYS_DIR="${BASE_DIR}/sys"

mkdir -p "${OPENWEBIF_DIR}" "${SYS_DIR}"

echo "=== xg2g Passive Diagnostic Collector v${COLLECTOR_VERSION} ==="
echo "Target OpenWebif: ${TARGET_URL}"
if [ -n "${SSH_TARGET}" ]; then
    echo "SSH Target: ${SSH_TARGET}"
else
    echo "SSH Target: None (Passive HTTP Only - proc/sys requires SSH)"
fi
echo "Output Directory: ${BASE_DIR}"
echo "Observation Time: ${TIMESTAMP}"
echo ""

PROBE_RESULTS="{}"

# Redaction helper: Redacts sensitive parameters, tokens, credentials, and IP addresses
redact_content() {
    sed -E \
        -e 's|http(s)?://[^:@]+:[^@]+@|http\1://[REDACTED_AUTH]@|g' \
        -e 's/("pin"|"password"|"token"|"auth"|"sessionid"|"pass"): *"[^"]+"/\1: "[REDACTED]"/g' \
        -e 's/([?&](pin|password|token|auth|pass)=)[^&]+/\1[REDACTED]/g' \
        -e 's/([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})/[REDACTED_EMAIL]/g' \
        -e 's/Authorization: [^\r\n]+/Authorization: [REDACTED]/g'
}

# Allowlisted OpenWebif Probe Runner
probe_http_endpoint() {
    local endpoint="$1"
    local filename="$2"
    local full_url="${TARGET_URL}${endpoint}"
    local out_file="${OPENWEBIF_DIR}/${filename}.json"
    local err_file="${OPENWEBIF_DIR}/${filename}.error.log"

    echo "[PASSIVE_HTTP] Probing ${endpoint}..."

    # Enforce strict bounded HTTP timeouts: connect 5s, max-time 10s
    if curl -sS --fail --connect-timeout 5 --max-time 10 "${full_url}" 2> "${err_file}" | redact_content > "${out_file}"; then
        rm -f "${err_file}"
        echo "  -> SUCCESS: ${filename}.json"
        PROBE_RESULTS="$(echo "${PROBE_RESULTS}" | jq ". + {\"${filename}\": \"SUCCESS\"}" 2>/dev/null || echo "${PROBE_RESULTS}")"
    else
        echo "  [PROBE_FAILED] ${endpoint} (see ${filename}.error.log)"
        rm -f "${out_file}"
        PROBE_RESULTS="$(echo "${PROBE_RESULTS}" | jq ". + {\"${filename}\": \"FAILED\"}" 2>/dev/null || echo "${PROBE_RESULTS}")"
    fi
}

echo "--- Phase 1: Passive OpenWebif HTTP Probes ---"
# Allowlist of read-only endpoints ONLY
probe_http_endpoint "/api/about" "about"
probe_http_endpoint "/api/deviceinfo" "deviceinfo"
probe_http_endpoint "/api/statusinfo" "statusinfo"
probe_http_endpoint "/api/tunersignal" "tunersignal"
probe_http_endpoint "/api/timerlist" "timerlist"
probe_http_endpoint "/api/getallservices" "getallservices"
probe_http_endpoint "/api/subservices" "subservices"
probe_http_endpoint "/api/getcurrent" "getcurrent"

# SSH Passive File Probe Runner (only executed if SSH_TARGET is provided)
if [ -n "${SSH_TARGET}" ]; then
    echo ""
    echo "--- Phase 1: Passive Kernel & System Probes via SSH ---"

    probe_ssh_read() {
        local remote_cmd="$1"
        local filename="$2"
        local out_file="${SYS_DIR}/${filename}.txt"
        local err_file="${SYS_DIR}/${filename}.error.log"

        echo "[PASSIVE_SSH] Reading ${filename}..."
        if ssh -o ConnectTimeout=5 -o BatchMode=yes "${SSH_TARGET}" "${remote_cmd}" 2> "${err_file}" | redact_content > "${out_file}"; then
            rm -f "${err_file}"
            echo "  -> SUCCESS: ${filename}.txt"
        else
            echo "  [PROBE_FAILED] ${filename} via SSH"
        fi
    }

    # Explicit allowlist of read-only file inspections
    probe_ssh_read "cat /etc/image-version /etc/issue 2>/dev/null; uname -a" "receiver_identity"
    probe_ssh_read "cat /proc/bus/nim_sockets 2>/dev/null" "nim_sockets"
    probe_ssh_read "find /sys/class/dvb -maxdepth 3 -type f 2>/dev/null | sort" "sys_dvb_class"
    probe_ssh_read "find /proc/stb/frontend -maxdepth 3 -type f 2>/dev/null | sort" "proc_stb_frontend"
    probe_ssh_read "ls -la /dev/dvb/adapter* 2>/dev/null" "dev_dvb_adapters"
    probe_ssh_read "cat /etc/enigma2/settings 2>/dev/null" "enigma2_settings"
fi

# Generate Collector Manifest
cat << EOF > "${BASE_DIR}/manifest.json"
{
  "collector_version": "${COLLECTOR_VERSION}",
  "timestamp_utc": "${TIMESTAMP}",
  "target_url": "${TARGET_URL}",
  "ssh_target": "${SSH_TARGET}",
  "probe_phase": "PASSIVE_COLLECTION",
  "probe_results": ${PROBE_RESULTS}
}
EOF

echo ""
echo "=== Passive Collection Complete ==="
echo "Manifest: ${BASE_DIR}/manifest.json"
echo "Output Directory: ${BASE_DIR}"
