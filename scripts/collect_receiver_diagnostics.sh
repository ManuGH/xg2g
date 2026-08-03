#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0

set -euo pipefail
umask 077

# Passive Diagnostic Collector (Phase 1 Baseline & Optional Phase 2 SSH)
# Purely read-only diagnostic collection for Enigma2 / OpenWebif.
# Strictly FORBIDDEN from performing zapping, streaming, timer creation,
# recording start, config mutation, or Enigma2 restarts.

COLLECTOR_VERSION="1.4.0"

# Determine script & repository root paths reliably regardless of PWD
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd -P)"

# -------------------------------------------------------------------
# STEP 1: Parse CLI Named Arguments
# -------------------------------------------------------------------
RAW_TARGET=""
CREDENTIALS_FILE=""
ENABLE_SSH=false
SSH_TARGET=""

usage() {
    echo "Usage: $0 --openwebif-url <URL> [--credentials-file <PATH>] [--enable-ssh --ssh-target <USER@HOST>]" >&2
    echo "Options:" >&2
    echo "  --openwebif-url <url>      Mandatory OpenWebif base URL (e.g. http://10.10.55.64)" >&2
    echo "  --credentials-file <path>   Optional credentials file (0600 permissions)" >&2
    echo "  --enable-ssh               Explicitly enable optional Phase 2 SSH probes" >&2
    echo "  --ssh-target <user@host>   SSH destination target (requires --enable-ssh)" >&2
}

# Support both named flags (--openwebif-url) and fallback positional parameters
if [[ $# -eq 0 ]]; then
    usage
    exit 1
fi

while [[ $# -gt 0 ]]; do
    case "$1" in
        --openwebif-url|-u)
            if [[ -z "${2:-}" ]]; then echo "ERROR: --openwebif-url requires a value!" >&2; exit 1; fi
            RAW_TARGET="$2"
            shift 2
            ;;
        --credentials-file|-c)
            if [[ -z "${2:-}" ]]; then echo "ERROR: --credentials-file requires a value!" >&2; exit 1; fi
            CREDENTIALS_FILE="$2"
            shift 2
            ;;
        --enable-ssh)
            ENABLE_SSH=true
            shift
            ;;
        --ssh-target|-s)
            if [[ -z "${2:-}" ]]; then echo "ERROR: --ssh-target requires a value!" >&2; exit 1; fi
            SSH_TARGET="$2"
            shift 2
            ;;
        --help|-h)
            usage
            exit 0
            ;;
        -*)
            echo "ERROR: Unknown option '$1'!" >&2
            usage
            exit 1
            ;;
        *)
            # Legacy positional fallback handling for backward compatibility
            if [ -z "${RAW_TARGET}" ]; then
                RAW_TARGET="$1"
            elif [ -z "${SSH_TARGET}" ]; then
                SSH_TARGET="$1"
            elif [ -z "${CREDENTIALS_FILE}" ]; then
                CREDENTIALS_FILE="$1"
            fi
            shift
            ;;
    esac
done

# -------------------------------------------------------------------
# STEP 2: Validate CLI Parameters BEFORE Creating Temp / Output Dirs
# -------------------------------------------------------------------
if [ -z "${RAW_TARGET}" ]; then
    echo "ERROR: Target OpenWebif URL must be specified!" >&2
    usage
    exit 1
fi

if [ -n "${SSH_TARGET}" ] && [ "${ENABLE_SSH}" = "false" ]; then
    echo "SECURITY ERROR: --ssh-target was supplied without explicit --enable-ssh flag!" >&2
    exit 1
fi

if [ "${ENABLE_SSH}" = "true" ] && [ -z "${SSH_TARGET}" ]; then
    echo "SECURITY ERROR: --enable-ssh was specified but --ssh-target is missing!" >&2
    exit 1
fi

# Mandatory dependency check: jq, curl, python3
for dep in jq curl python3; do
    if ! command -v "${dep}" >/dev/null 2>&1; then
        echo "ERROR: Required dependency '${dep}' is missing." >&2
        exit 1
    fi
done

# Strict URL & Target Validation via Python urllib.parse
PARSED_URL_JSON="$(python3 -c '
import sys, json
from urllib.parse import urlparse, parse_qs

raw = sys.argv[1].strip()
if "://" in raw:
    if not raw.startswith(("http://", "https://")):
        print(json.dumps({"error": f"Invalid scheme in URL: {raw}"}))
        sys.exit(1)
else:
    raw = "http://" + raw

try:
    p = urlparse(raw)
    port = p.port
except Exception as e:
    print(json.dumps({"error": f"Invalid URL or port: {e}"}))
    sys.exit(1)

if p.scheme not in ("http", "https"):
    print(json.dumps({"error": f"Invalid scheme: {p.scheme}"}))
    sys.exit(1)

if not p.hostname:
    print(json.dumps({"error": "Missing hostname in URL"}))
    sys.exit(1)

if p.username or p.password:
    print(json.dumps({"error": "URLs with embedded credentials (user:password@) are strictly forbidden!"}))
    sys.exit(1)

if p.fragment:
    print(json.dumps({"error": "URL fragments (#) are not supported"}))
    sys.exit(1)

if port is None:
    port = 443 if p.scheme == "https" else 80

if port < 1 or port > 65535:
    print(json.dumps({"error": f"Invalid port: {port}"}))
    sys.exit(1)

# Check query parameters for sensitive keys
forbidden_keys = {"token", "access_token", "auth", "jwt", "bearer", "password", "pass", "sessionid", "pin", "secret", "key"}
qs = parse_qs(p.query)
query_keys = {k.lower() for k in qs.keys()}
matched_forbidden = query_keys.intersection(forbidden_keys)
if matched_forbidden:
    print(json.dumps({"error": f"Target URL contains sensitive query parameters: {sorted(list(matched_forbidden))}"}))
    sys.exit(1)

target_url = f"{p.scheme}://{p.hostname}"
if (p.scheme == "http" and port != 80) or (p.scheme == "https" and port != 443):
    target_url += f":{port}"
if p.path and p.path != "/":
    target_url += p.path.rstrip("/")

print(json.dumps({
    "scheme": p.scheme,
    "hostname": p.hostname,
    "port": port,
    "target_url": target_url
}))
' "${RAW_TARGET}")"

URL_ERR="$(echo "${PARSED_URL_JSON}" | jq -r '.error // empty')"
if [ -n "${URL_ERR}" ]; then
    echo "SECURITY ERROR: ${URL_ERR}" >&2
    exit 1
fi

SCHEME="$(echo "${PARSED_URL_JSON}" | jq -r '.scheme')"
PORT="$(echo "${PARSED_URL_JSON}" | jq -r '.port')"
TARGET_URL="$(echo "${PARSED_URL_JSON}" | jq -r '.target_url')"

# Validate SSH_TARGET if enabled
if [ "${ENABLE_SSH}" = "true" ]; then
    if [[ "${SSH_TARGET}" =~ ^- ]]; then
        echo "SECURITY ERROR: SSH target must not start with a dash (-)!" >&2
        exit 1
    fi
    if [[ "${SSH_TARGET}" =~ [[:space:]\'\"\`\$] ]]; then
        echo "SECURITY ERROR: SSH target contains illegal whitespace or shell control characters!" >&2
        exit 1
    fi
    if ! [[ "${SSH_TARGET}" =~ ^([a-zA-Z0-9._-]+@)?[a-zA-Z0-9._-]+$ ]]; then
        echo "SECURITY ERROR: Invalid SSH target format! Must match [user@]host." >&2
        exit 1
    fi
fi

# Validate Credentials File if provided
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

    # Check for forbidden curl configuration options
    if grep -iE '^\s*(url|upload-file|data|request|output|proxy|header|cookie)\s*=' "${CREDENTIALS_FILE}" >/dev/null 2>&1; then
        echo "SECURITY ERROR: Credentials file contains illegal curl configuration options!" >&2
        exit 1
    fi

    HAS_KV=false
    HAS_NETRC=false
    if grep -E '^\s*(username|user|login|password|pass)\s*=' "${CREDENTIALS_FILE}" >/dev/null 2>&1; then
        HAS_KV=true
    fi
    if grep -E '^\s*(machine|default|login|password)\b' "${CREDENTIALS_FILE}" | grep -v '=' >/dev/null 2>&1; then
        HAS_NETRC=true
    fi

    if [ "${HAS_KV}" = "true" ] && [ "${HAS_NETRC}" = "true" ]; then
        echo "SECURITY ERROR: Mixed credentials format detected! Must be strictly Key-Value OR Netrc format." >&2
        exit 1
    fi

    # Validate non-empty, non-comment lines
    CLEAN_LINES="$(grep -v '^\s*$' "${CREDENTIALS_FILE}" | grep -v '^\s*#' || true)"
    if [ -z "${CLEAN_LINES}" ]; then
        echo "SECURITY ERROR: Credentials file is empty or contains only comments!" >&2
        exit 1
    fi

    if [ "${HAS_KV}" = "true" ]; then
        INVALID_LINES="$(echo "${CLEAN_LINES}" | grep -vE '^\s*(username|user|login|password|pass)\s*=' || true)"
        if [ -n "${INVALID_LINES}" ]; then
            echo "SECURITY ERROR: Credentials file contains unknown or invalid key-value pairs!" >&2
            exit 1
        fi

        # Check for duplicate key assignments
        USER_COUNT="$(echo "${CLEAN_LINES}" | grep -cE '^\s*(username|user|login)\s*=' || true)"
        PASS_COUNT="$(echo "${CLEAN_LINES}" | grep -cE '^\s*(password|pass)\s*=' || true)"
        if [ "${USER_COUNT}" -gt 1 ] || [ "${PASS_COUNT}" -gt 1 ]; then
            echo "SECURITY ERROR: Duplicate username or password keys in credentials file!" >&2
            exit 1
        fi

        USER_VAL="$(echo "${CLEAN_LINES}" | grep -E '^\s*(username|user|login)\s*=' | head -n 1 | sed -E 's/^\s*(username|user|login)\s*=\s*//')"
        PASS_VAL="$(echo "${CLEAN_LINES}" | grep -E '^\s*(password|pass)\s*=' | head -n 1 | sed -E 's/^\s*(password|pass)\s*=\s*//')"

        if [ -z "${USER_VAL}" ] || [ -z "${PASS_VAL}" ]; then
            echo "SECURITY ERROR: Key-Value credentials file must specify both username and password!" >&2
            exit 1
        fi

        # Reject spaces, quotes, control characters, or backslashes
        if [[ "${USER_VAL}" =~ [[:space:]\'\"\\] ]] || [[ "${PASS_VAL}" =~ [[:space:]\'\"\\] ]]; then
            echo "SECURITY ERROR: Credential values containing spaces, quotes, or backslashes are forbidden in netrc format." >&2
            exit 1
        fi
    fi
fi

# -------------------------------------------------------------------
# STEP 3: Setup Transient Temp Dir & Output Directory AFTER Validation
# -------------------------------------------------------------------
TEMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/xg2g_collector_XXXXXX")"
chmod 700 "${TEMP_DIR}"

cleanup() {
    rm -rf "${TEMP_DIR}"
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
trap 'cleanup; exit 129' HUP

SECURE_NETRC="${TEMP_DIR}/collector.netrc"
if [ -n "${CREDENTIALS_FILE}" ]; then
    if [ "${HAS_KV:-false}" = "true" ]; then
        cat << EOF > "${SECURE_NETRC}"
default
login ${USER_VAL}
password ${PASS_VAL}
EOF
    else
        cp "${CREDENTIALS_FILE}" "${SECURE_NETRC}"
    fi
    chmod 600 "${SECURE_NETRC}"
    CURL_AUTH_ARGS=(--netrc-file "${SECURE_NETRC}")
fi

TIMESTAMP="$(date -u +"%Y%m%dT%H%M%SZ")"

if command -v shasum >/dev/null 2>&1; then
    PSEUDONYM_FINGERPRINT="$(printf "%s" "${TARGET_URL}" | shasum -a 256 | awk '{print $1}')"
else
    PSEUDONYM_FINGERPRINT="$(printf "%s" "${TARGET_URL}" | sha256sum | awk '{print $1}')"
fi

BASE_DIR="${REPO_ROOT}/var/diagnostics/enigma2/${TIMESTAMP}"
OPENWEBIF_DIR="${BASE_DIR}/openwebif"
mkdir -p "${OPENWEBIF_DIR}"

SSH_EXECUTED=false
if [ "${ENABLE_SSH}" = "true" ]; then
    SYS_DIR="${BASE_DIR}/sys"
    mkdir -p "${SYS_DIR}"
    SSH_EXECUTED=true
fi

echo "=== xg2g Passive Diagnostic Collector v${COLLECTOR_VERSION} ==="
echo "Scheme: ${SCHEME} | Port: ${PORT}"
echo "Pseudonym Fingerprint: ${PSEUDONYM_FINGERPRINT:0:16}..."
echo "OpenWebif Enabled: true"
echo "SSH Requested: ${ENABLE_SSH}"
echo "SSH Executed: ${SSH_EXECUTED}"
echo "Output Directory: ${BASE_DIR}"
echo "Observation Time: ${TIMESTAMP}"
echo ""

PROBES_JSON="[]"

redact_content() {
    sed -E \
        -e 's|http(s)?://[^:@]+:[^@]+@|http\1://[REDACTED_AUTH]@|g' \
        -e 's/("pin"|"password"|"token"|"auth"|"sessionid"|"pass"): *"[^"]+"/\1: "[REDACTED]"/g' \
        -e 's/(([?&; \t]|^)(pin|password|token|auth|sessionid|pass)=)[^" \t\r\n&;]+/\1[REDACTED]/g' \
        -e 's/(Bearer )[^\r\n" \t&;]+/\1[REDACTED]/g' \
        -e 's/secret_[a-zA-Z0-9_]+/[REDACTED]/g' \
        -e 's/([a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})/[REDACTED_EMAIL]/g' \
        -e 's/Authorization: [^\r\n]+/Authorization: [REDACTED]/g' \
        -e 's/([0-9]{1,3}\.){3}[0-9]{1,3}/[REDACTED_IP]/g'
}

get_timestamp_ms() {
    python3 -c 'import time; print(int(time.time() * 1000))'
}

probe_http_endpoint() {
    local probe_id="$1"
    local endpoint="$2"
    local full_url="${TARGET_URL}${endpoint}"
    local out_file="${OPENWEBIF_DIR}/${probe_id}.json"
    local err_file="${OPENWEBIF_DIR}/${probe_id}.error.log"
    local raw_out_file="${TEMP_DIR}/${probe_id}.tmp.out"
    local raw_err_file="${TEMP_DIR}/${probe_id}.tmp.err"
    local raw_meta_file="${TEMP_DIR}/${probe_id}.tmp.meta"

    echo "[PASSIVE_HTTP] Probing ${endpoint}..."
    local start_time
    start_time="$(get_timestamp_ms)"

    local curl_exit=0
    local http_status=0
    local content_type="unknown"
    local status="FAILED"
    local rel_out_path=""
    local rel_err_path=""

    curl -sS \
        "${CURL_AUTH_ARGS[@]}" \
        --connect-timeout 5 \
        --max-time 10 \
        -w "%{http_code}\n%{content_type}" \
        "${full_url}" \
        -o "${raw_out_file}" \
        2>"${raw_err_file}" \
        >"${raw_meta_file}" || curl_exit=$?

    if [ ${curl_exit} -eq 130 ] || [ ${curl_exit} -eq 143 ] || [ ${curl_exit} -eq 129 ]; then
        cleanup
        exit ${curl_exit}
    fi

    local end_time
    end_time="$(get_timestamp_ms)"
    local duration_ms=$((end_time - start_time))

    if [ ${curl_exit} -eq 0 ] && [ -f "${raw_meta_file}" ]; then
        http_status="$(head -n 1 "${raw_meta_file}" | tr -d '\r\n' || echo "000")"
        local raw_ct
        raw_ct="$(sed -n '2p' "${raw_meta_file}" | cut -d';' -f1 | tr -d '\r\n' || echo "unknown")"
        if [ -n "${raw_ct}" ]; then
            content_type="${raw_ct}"
        fi
    else
        http_status=0
        content_type="unknown"
    fi

    if [ "${http_status}" = "200" ]; then
        status="SUCCESS"
        redact_content < "${raw_out_file}" > "${out_file}"
        rm -f "${raw_out_file}" "${raw_err_file}" "${raw_meta_file}" "${err_file}"
        rel_out_path="openwebif/${probe_id}.json"
        echo "  -> SUCCESS (HTTP 200, ${content_type}, ${duration_ms}ms)"
    else
        status="FAILED"
        {
            echo "--- HTTP Response Code: ${http_status} ---"
            echo "--- Content-Type: ${content_type} ---"
            if [ -f "${raw_err_file}" ] && [ -s "${raw_err_file}" ]; then
                echo "--- STDERR ---"
                cat "${raw_err_file}"
            fi
            if [ -f "${raw_out_file}" ] && [ -s "${raw_out_file}" ]; then
                echo "--- RESPONSE BODY ---"
                cat "${raw_out_file}"
            fi
        } | redact_content > "${err_file}"

        rm -f "${raw_out_file}" "${raw_err_file}" "${raw_meta_file}"
        rel_err_path="openwebif/${probe_id}.error.log"
        echo "  [PROBE_FAILED] ${endpoint} (HTTP ${http_status}, ${content_type}, see ${probe_id}.error.log)"
    fi

    PROBES_JSON="$(jq -n \
        --argjson probes "${PROBES_JSON}" \
        --arg id "${probe_id}" \
        --arg kind "HTTP_GET" \
        --arg status "${status}" \
        --argjson duration "${duration_ms}" \
        --argjson http_code "${http_status}" \
        --arg ctype "${content_type}" \
        --arg out_file "${rel_out_path}" \
        --arg err_file "${rel_err_path}" \
        '$probes + [{
            probe_id: $id,
            kind: $kind,
            status: $status,
            duration_ms: $duration,
            http_status: $http_code,
            content_type: $ctype,
            output_file: (if $out_file == "" then null else $out_file end),
            error_file: (if $err_file == "" then null else $err_file end)
        }]')"
}

echo "--- Phase 1: Passive OpenWebif HTTP Probes ---"
probe_http_endpoint "about" "/api/about"
probe_http_endpoint "deviceinfo" "/api/deviceinfo"
probe_http_endpoint "statusinfo" "/api/statusinfo"
probe_http_endpoint "tunersignal" "/api/tunersignal"
probe_http_endpoint "timerlist" "/api/timerlist"
probe_http_endpoint "getallservices" "/api/getallservices"
probe_http_endpoint "subservices" "/api/subservices"
probe_http_endpoint "getcurrent" "/api/getcurrent"

# Phase 2: SSH Probes (ONLY executed if --enable-ssh was explicitly supplied)
if [ "${ENABLE_SSH}" = "true" ] && [ -n "${SSH_TARGET}" ]; then
    echo ""
    echo "--- Phase 2: Optional Passive Kernel & System Probes via SSH ---"

    probe_ssh_read() {
        local probe_id="$1"
        local remote_cmd="$2"
        local out_file="${SYS_DIR}/${probe_id}.txt"
        local err_file="${SYS_DIR}/${probe_id}.error.log"
        local raw_out_file="${TEMP_DIR}/${probe_id}.tmp.out"
        local raw_err_file="${TEMP_DIR}/${probe_id}.tmp.err"

        echo "[PASSIVE_SSH] Reading ${probe_id}..."
        local start_time
        start_time="$(get_timestamp_ms)"

        local status="FAILED"
        local rel_out_path=""
        local rel_err_path=""

        if timeout 15s ssh \
            -o BatchMode=yes \
            -o ConnectTimeout=5 \
            -o ServerAliveInterval=5 \
            -o ServerAliveCountMax=2 \
            -- \
            "${SSH_TARGET}" "${remote_cmd}" > "${raw_out_file}" 2> "${raw_err_file}"; then
            
            status="SUCCESS"
            redact_content < "${raw_out_file}" > "${out_file}"
            rm -f "${raw_out_file}" "${raw_err_file}" "${err_file}"
            rel_out_path="sys/${probe_id}.txt"
            echo "  -> SUCCESS: sys/${probe_id}.txt"
        else
            status="FAILED"
            {
                echo "--- SSH PROBE FAILED ---"
                if [ -f "${raw_out_file}" ] && [ -s "${raw_out_file}" ]; then
                    echo "--- STDOUT ---"
                    cat "${raw_out_file}"
                fi
                if [ -f "${raw_err_file}" ] && [ -s "${raw_err_file}" ]; then
                    echo "--- STDERR ---"
                    cat "${raw_err_file}"
                fi
            } | redact_content > "${err_file}"
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
    --arg fingerprint "${PSEUDONYM_FINGERPRINT}" \
    --argjson openwebif_enabled true \
    --argjson ssh_requested "${ENABLE_SSH}" \
    --argjson ssh_executed "${SSH_EXECUTED}" \
    --arg phase "PASSIVE_COLLECTION" \
    --argjson probes "${PROBES_JSON}" \
    '{
        collector_version: $version,
        timestamp_utc: $timestamp,
        scheme: $scheme,
        port: $port,
        pseudonym_fingerprint: $fingerprint,
        openwebif_enabled: $openwebif_enabled,
        ssh_requested: $ssh_requested,
        ssh_executed: $ssh_executed,
        probe_phase: $phase,
        probes: $probes
    }' > "${BASE_DIR}/manifest.json"

echo ""
echo "=== Passive Collection Complete ==="
echo "Manifest: ${BASE_DIR}/manifest.json"
echo "Output Directory: ${BASE_DIR}"
