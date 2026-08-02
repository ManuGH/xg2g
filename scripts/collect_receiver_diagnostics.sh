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

# Mandatory dependency check: jq MUST be present
if ! command -v jq >/dev/null 2>&1; then
    echo "ERROR: 'jq' is required for safe JSON manifest generation but is missing." >&2
    exit 1
fi

# Usage check: Target MUST be passed explicitly. No default IP allowed.
if [ -z "${1:-}" ]; then
    echo "ERROR: Target OpenWebif URL or host must be specified explicitly!" >&2
    echo "Usage: $0 <OPENWEBIF_HOST_OR_URL> [SSH_TARGET] [CREDENTIALS_FILE]" >&2
    exit 1
fi

RAW_TARGET="$1"
SSH_TARGET="${2:-}"
CREDENTIALS_FILE="${3:-}"

# SECURITY RULE 1: Reject credential-bearing URLs immediately (e.g. http://user:pass@host)
if [[ "${RAW_TARGET}" =~ @ ]]; then
    echo "SECURITY ERROR: URLs with embedded credentials (user:password@) are strictly forbidden!" >&2
    echo "Command-line arguments are visible in process lists and shell history." >&2
    echo "Use a secure credentials file (0600 permissions) via the 3rd argument instead." >&2
    exit 1
fi

TIMESTAMP="$(date -u +"%Y%m%dT%H%M%SZ")"

# Normalize Target URL
TARGET_URL="${RAW_TARGET}"
if [[ "${TARGET_URL}" != http://* ]] && [[ "${TARGET_URL}" != https://* ]]; then
    TARGET_URL="http://${RAW_TARGET}"
fi
TARGET_URL="${TARGET_URL%/}"

# Extract scheme and port for safe, unidentifiable metadata logging
SCHEME="$(echo "${TARGET_URL}" | grep -oE '^(http|https)')"
PORT="$(echo "${TARGET_URL}" | sed -E 's|^https?://[^/:]+:?([0-9]*).*|\1|')"
if [ -z "${PORT}" ]; then
    if [ "${SCHEME}" = "https" ]; then PORT="443"; else PORT="80"; fi
fi

# Compute SHA-256 fingerprint of target URL (never output or log raw target IP/host)
if command -v shasum >/dev/null 2>&1; then
    TARGET_FINGERPRINT="$(printf "%s" "${TARGET_URL}" | shasum -a 256 | awk '{print $1}')"
else
    TARGET_FINGERPRINT="$(printf "%s" "${TARGET_URL}" | sha256sum | awk '{print $1}')"
fi

SSH_ENABLED=false
if [ -n "${SSH_TARGET}" ]; then
    SSH_ENABLED=true
fi

# Base output directory - ALWAYS in gitignored var/diagnostics/
BASE_DIR="$(pwd)/var/diagnostics/enigma2/${TIMESTAMP}"
OPENWEBIF_DIR="${BASE_DIR}/openwebif"
SYS_DIR="${BASE_DIR}/sys"

mkdir -p "${OPENWEBIF_DIR}" "${SYS_DIR}"

echo "=== xg2g Passive Diagnostic Collector v${COLLECTOR_VERSION} ==="
echo "Scheme: ${SCHEME} | Port: ${PORT}"
echo "Receiver Fingerprint: ${TARGET_FINGERPRINT:0:16}..."
echo "SSH Enabled: ${SSH_ENABLED}"
echo "Output Directory: ${BASE_DIR}"
echo "Observation Time: ${TIMESTAMP}"
echo ""

# Handle optional Credentials File safely (0600 permissions required)
CURL_AUTH_ARGS=()
if [ -n "${CREDENTIALS_FILE}" ]; then
    if [ ! -f "${CREDENTIALS_FILE}" ]; then
        echo "ERROR: Specified credentials file '${CREDENTIALS_FILE}' does not exist!" >&2
        exit 1
    fi
    PERMS="$(stat -f "%Lp" "${CREDENTIALS_FILE}" 2>/dev/null || stat -c "%a" "${CREDENTIALS_FILE}" 2>/dev/null || echo "000")"
    if [ "${PERMS}" != "600" ] && [ "${PERMS}" != "400" ]; then
        echo "SECURITY ERROR: Credentials file permissions must be 0600 or 0400 (found ${PERMS})!" >&2
        exit 1
    fi
    CURL_AUTH_ARGS=(--config "${CREDENTIALS_FILE}")
fi

# Initialize empty JSON array for structured probe entries
PROBES_JSON="[]"

# Comprehensive Redaction helper for STDOUT and STDERR
redact_content() {
    sed -E \
        -e 's|http(s)?://[^:@]+:[^@]+@|http\1://[REDACTED_AUTH]@|g' \
        -e 's/("pin"|"password"|"token"|"auth"|"sessionid"|"pass"): *"[^"]+"/\1: "[REDACTED]"/g' \
        -e 's/([?&](pin|password|token|auth|pass)=)[^&]+/\1[REDACTED]/g' \
        -e 's/([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})/[REDACTED_EMAIL]/g' \
        -e 's/Authorization: [^\r\n]+/Authorization: [REDACTED]/g' \
        -e 's/([0-9]{1,3}\.){3}[0-9]{1,3}/[REDACTED_IP]/g'
}

# Portable millisecond timestamp helper
get_timestamp_ms() {
    if command -v python3 >/dev/null 2>&1; then
        python3 -c 'import time; print(int(time.time() * 1000))'
    elif command -v perl >/dev/null 2>&1; then
        perl -MTime::HiRes=time -e 'printf("%d\n", time()*1000)'
    else
        echo "$(date +%s)000"
    fi
}

# Allowlisted OpenWebif Probe Runner
probe_http_endpoint() {
    local probe_id="$1"
    local endpoint="$2"
    local full_url="${TARGET_URL}${endpoint}"
    local out_file="${OPENWEBIF_DIR}/${probe_id}.json"
    local err_file="${OPENWEBIF_DIR}/${probe_id}.error.log"
    local raw_out_file="${OPENWEBIF_DIR}/${probe_id}.tmp.out"
    local raw_err_file="${OPENWEBIF_DIR}/${probe_id}.tmp.err"

    echo "[PASSIVE_HTTP] Probing ${endpoint}..."
    local start_time
    start_time="$(get_timestamp_ms)"

    local http_status=0
    local status="FAILED"
    local rel_out_path=""
    local rel_err_path=""

    # Enforce strict bounded HTTP timeouts: connect 5s, max-time 10s
    # Capture HTTP status code cleanly via -w "%{http_code}"
    http_status="$(curl -sS "${CURL_AUTH_ARGS[@]}" -w "%{http_code}" --connect-timeout 5 --max-time 10 "${full_url}" -o "${raw_out_file}" 2> "${raw_err_file}" || echo "000")"

    local end_time
    end_time="$(get_timestamp_ms)"
    local duration_ms=$((end_time - start_time))

    # Redact error log safely
    redact_content < "${raw_err_file}" > "${err_file}"
    rm -f "${raw_err_file}"

    if [ "${http_status}" = "200" ]; then
        status="SUCCESS"
        redact_content < "${raw_out_file}" > "${out_file}"
        rm -f "${raw_out_file}" "${err_file}"
        rel_out_path="openwebif/${probe_id}.json"
        echo "  -> SUCCESS (HTTP 200, ${duration_ms}ms)"
    else
        status="FAILED"
        rm -f "${raw_out_file}"
        rel_err_path="openwebif/${probe_id}.error.log"
        echo "  [PROBE_FAILED] ${endpoint} (HTTP ${http_status}, see ${probe_id}.error.log)"
    fi

    # Append structured probe record via safe jq construction
    PROBES_JSON="$(jq -n \
        --argjson probes "${PROBES_JSON}" \
        --arg id "${probe_id}" \
        --arg kind "HTTP_GET" \
        --arg status "${status}" \
        --argjson duration "${duration_ms}" \
        --argjson http_code "${http_status}" \
        --arg content_type "application/json" \
        --arg out_file "${rel_out_path}" \
        --arg err_file "${rel_err_path}" \
        '$probes + [{
            probe_id: $id,
            kind: $kind,
            status: $status,
            duration_ms: $duration,
            http_status: $http_code,
            content_type: $content_type,
            output_file: (if $out_file == "" then null else $out_file end),
            error_file: (if $err_file == "" then null else $err_file end)
        }]')"
}

echo "--- Phase 1: Passive OpenWebif HTTP Probes ---"
# Allowlist of read-only endpoints ONLY
probe_http_endpoint "about" "/api/about"
probe_http_endpoint "deviceinfo" "/api/deviceinfo"
probe_http_endpoint "statusinfo" "/api/statusinfo"
probe_http_endpoint "tunersignal" "/api/tunersignal"
probe_http_endpoint "timerlist" "/api/timerlist"
probe_http_endpoint "getallservices" "/api/getallservices"
probe_http_endpoint "subservices" "/api/subservices"
probe_http_endpoint "getcurrent" "/api/getcurrent"

# SSH Passive File Probe Runner (only executed if SSH_TARGET is provided)
if [ -n "${SSH_TARGET}" ]; then
    echo ""
    echo "--- Phase 1: Passive Kernel & System Probes via SSH ---"

    probe_ssh_read() {
        local probe_id="$1"
        local remote_cmd="$2"
        local out_file="${SYS_DIR}/${probe_id}.txt"
        local err_file="${SYS_DIR}/${probe_id}.error.log"
        local raw_out_file="${SYS_DIR}/${probe_id}.tmp.out"
        local raw_err_file="${SYS_DIR}/${probe_id}.tmp.err"

        echo "[PASSIVE_SSH] Reading ${probe_id}..."
        local start_time
        start_time="$(get_timestamp_ms)"

        local status="FAILED"
        local rel_out_path=""
        local rel_err_path=""

        # Enforce bounded SSH timeout: timeout 15s ssh ...
        if timeout 15s ssh \
            -o BatchMode=yes \
            -o ConnectTimeout=5 \
            -o ServerAliveInterval=5 \
            -o ServerAliveCountMax=2 \
            "${SSH_TARGET}" "${remote_cmd}" > "${raw_out_file}" 2> "${raw_err_file}"; then
            
            status="SUCCESS"
            redact_content < "${raw_out_file}" > "${out_file}"
            rm -f "${raw_out_file}" "${raw_err_file}" "${err_file}"
            rel_out_path="sys/${probe_id}.txt"
            echo "  -> SUCCESS: sys/${probe_id}.txt"
        else
            status="FAILED"
            redact_content < "${raw_err_file}" > "${err_file}"
            rm -f "${raw_out_file}" "${raw_err_file}"
            rel_err_path="sys/${probe_id}.error.log"
            echo "  [PROBE_FAILED] ${probe_id} via SSH (see sys/${probe_id}.error.log)"
        fi

        local end_time
        end_time="$(get_timestamp_ms)"
        local duration_ms=$((end_time - start_time))

        PROBES_JSON="$(jq -n \
            --argjson probes "${PROBES_JSON}" \
            --arg id "${probe_id}" \
            --arg kind "SSH_READ" \
            --arg status "${status}" \
            --argjson duration "${duration_ms}" \
            --arg out_file "${rel_out_path}" \
            --arg err_file "${rel_err_path}" \
            '$probes + [{
                probe_id: $id,
                kind: $kind,
                status: $status,
                duration_ms: $duration,
                http_status: 0,
                content_type: "text/plain",
                output_file: (if $out_file == "" then null else $out_file end),
                error_file: (if $err_file == "" then null else $err_file end)
            }]')"
    }

    # Explicit allowlist of read-only file inspections (filtering /etc/enigma2/settings for tuner/SEC keys ONLY)
    probe_ssh_read "receiver_identity" "cat /etc/image-version /etc/issue 2>/dev/null; uname -a"
    probe_ssh_read "nim_sockets" "cat /proc/bus/nim_sockets 2>/dev/null"
    probe_ssh_read "sys_dvb_class" "find /sys/class/dvb -maxdepth 3 -type f 2>/dev/null | sort"
    probe_ssh_read "proc_stb_frontend" "find /proc/stb/frontend -maxdepth 3 -type f 2>/dev/null | sort"
    probe_ssh_read "dev_dvb_adapters" "ls -la /dev/dvb/adapter* 2>/dev/null"
    probe_ssh_read "enigma2_tuner_settings" "grep -E '^(config.Nims|config.sec|config.unicable|config.sat)' /etc/enigma2/settings 2>/dev/null"
fi

# Generate Collector Manifest safely using jq
jq -n \
    --arg version "${COLLECTOR_VERSION}" \
    --arg timestamp "${TIMESTAMP}" \
    --arg scheme "${SCHEME}" \
    --argjson port "${PORT}" \
    --arg fingerprint "${TARGET_FINGERPRINT}" \
    --argjson ssh_enabled "${SSH_ENABLED}" \
    --arg phase "PASSIVE_COLLECTION" \
    --argjson probes "${PROBES_JSON}" \
    '{
        collector_version: $version,
        timestamp_utc: $timestamp,
        scheme: $scheme,
        port: $port,
        receiver_fingerprint: $fingerprint,
        ssh_enabled: $ssh_enabled,
        probe_phase: $phase,
        probes: $probes
    }' > "${BASE_DIR}/manifest.json"

echo ""
echo "=== Passive Collection Complete ==="
echo "Manifest: ${BASE_DIR}/manifest.json"
echo "Output Directory: ${BASE_DIR}"
