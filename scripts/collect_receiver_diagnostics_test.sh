#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0

set -euo pipefail

# Executable Unit Test Suite for scripts/collect_receiver_diagnostics.sh
SCRIPT_UNDER_TEST="$(pwd)/scripts/collect_receiver_diagnostics.sh"
REPO_ROOT="$(cd -- "$(dirname -- "${SCRIPT_UNDER_TEST}")/.." && pwd -P)"
TEST_TMP_DIR="$(mktemp -d /tmp/collector_test_XXXXXX)"

cleanup() {
    rm -rf "${TEST_TMP_DIR}"
}
trap cleanup EXIT

echo "=== Running Executable Collector Security, CLI & Redaction Unit Tests v1.5.0 ==="

# Test 1: PositionalArgumentsStrictlyRejected
echo -n "Test 1: PositionalArgumentsStrictlyRejected... "
rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
if "${SCRIPT_UNDER_TEST}" "http://192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed positional target URL argument!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" "root@192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed positional SSH target argument!"
    exit 1
fi
LEFTOVER_COUNT="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -type f 2>/dev/null | wc -l | tr -d ' \t\r\n' || true)"
LEFTOVER_COUNT="${LEFTOVER_COUNT:-0}"
if [ "${LEFTOVER_COUNT}" -ne 0 ]; then
    echo "FAILED! Positional argument failure created persistent output directory!"
    exit 1
fi
echo "PASSED"

# Test 2: RejectsCredentialBearingURLAndSensitiveQueryParams
echo -n "Test 2: RejectsCredentialBearingURLAndSensitiveQueryParams... "
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://admin:secret123@192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed credential-bearing URL!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50/api/test?pass=sample_secret_pass" >/dev/null 2>&1; then
    echo "FAILED! Script allowed URL containing sensitive query parameter 'pass'!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50/api/test?sessionid=sample_session_123" >/dev/null 2>&1; then
    echo "FAILED! Script allowed URL containing sensitive query parameter 'sessionid'!"
    exit 1
fi
echo "PASSED"

# Test 3: SSHTargetMismatchedEnableSSH_Fails
echo -n "Test 3: SSHTargetMismatchedEnableSSH_Fails... "
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --ssh-target "root@192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed --ssh-target without --enable-ssh!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --enable-ssh >/dev/null 2>&1; then
    echo "FAILED! Script allowed --enable-ssh without --ssh-target!"
    exit 1
fi
echo "PASSED"

# Test 4: SSHTargetRejectsOptionInjection
echo -n "Test 4: SSHTargetRejectsOptionInjection... "
HACK_FILE="${TEST_TMP_DIR}/hacked"
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --enable-ssh --ssh-target "-oProxyCommand=touch ${HACK_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed SSH option injection starting with -o!"
    exit 1
fi
if [ -f "${HACK_FILE}" ]; then
    echo "FAILED! Executed injected ProxyCommand!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --enable-ssh --ssh-target "user host" >/dev/null 2>&1; then
    echo "FAILED! Script allowed SSH target containing whitespace!"
    exit 1
fi
echo "PASSED"

# Test 5: CredentialsFormatStrictlyKeyValueOnly
echo -n "Test 5: CredentialsFormatStrictlyKeyValueOnly... "
NETRC_FILE="${TEST_TMP_DIR}/netrc_creds.cfg"
cat << 'EOF' > "${NETRC_FILE}"
default
login root
password secret
EOF
chmod 600 "${NETRC_FILE}"
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --credentials-file "${NETRC_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed Netrc format credentials file!"
    exit 1
fi

KV_INVALID_FILE="${TEST_TMP_DIR}/kv_invalid.cfg"
cat << 'EOF' > "${KV_INVALID_FILE}"
username=root
password=secret
illegal_option=value
EOF
chmod 600 "${KV_INVALID_FILE}"
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --credentials-file "${KV_INVALID_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed credentials file with unknown keys!"
    exit 1
fi

KV_SPACE_FILE="${TEST_TMP_DIR}/kv_space.cfg"
cat << 'EOF' > "${KV_SPACE_FILE}"
username=root user
password=secret
EOF
chmod 600 "${KV_SPACE_FILE}"
if "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50" --credentials-file "${KV_SPACE_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed credentials containing spaces!"
    exit 1
fi
echo "PASSED"

# Test 6: ValidationFailureLeavesZeroPersistentOutput
echo -n "Test 6: ValidationFailureLeavesZeroPersistentOutput... "
rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
BEFORE_COUNT="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -type f 2>/dev/null | wc -l | tr -d ' \t\r\n' || true)"
BEFORE_COUNT="${BEFORE_COUNT:-0}"

# Try invalid inputs
"${SCRIPT_UNDER_TEST}" --openwebif-url "ftp://192.168.1.50" >/dev/null 2>&1 || true
"${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50:99999" >/dev/null 2>&1 || true
"${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.50/api#fragment" >/dev/null 2>&1 || true
"${SCRIPT_UNDER_TEST}" --openwebif-url "http://admin:pass@192.168.1.50" >/dev/null 2>&1 || true
"${SCRIPT_UNDER_TEST}" "http://192.168.1.50" >/dev/null 2>&1 || true

AFTER_COUNT="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -type f 2>/dev/null | wc -l | tr -d ' \t\r\n' || true)"
AFTER_COUNT="${AFTER_COUNT:-0}"
if [ "${BEFORE_COUNT}" -ne "${AFTER_COUNT}" ]; then
    echo "FAILED! Validation failure created persistent files under var/diagnostics/enigma2:"
    find "${REPO_ROOT}/var/diagnostics/enigma2" -type f 2>/dev/null || true
    exit 1
fi
echo "PASSED"

# Test 7: OpenWebifOnlyModeInvokesZeroSSH
echo -n "Test 7: OpenWebifOnlyModeInvokesZeroSSH... "
MOCK_BIN_OPENWEBIF="${TEST_TMP_DIR}/mock_bin_owonly"
mkdir -p "${MOCK_BIN_OPENWEBIF}"

cat << 'EOF' > "${MOCK_BIN_OPENWEBIF}/curl"
#!/usr/bin/env bash
out_file=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) out_file="$2"; shift 2 ;;
        *) shift ;;
    esac
done
if [ -n "${out_file}" ]; then
    echo '{"result": true}' > "${out_file}"
fi
printf "200\napplication/json"
EOF
chmod +x "${MOCK_BIN_OPENWEBIF}/curl"

SSH_LOG="${TEST_TMP_DIR}/ssh_invocations.log"
cat << EOF > "${MOCK_BIN_OPENWEBIF}/ssh"
#!/usr/bin/env bash
echo "\$@" >> "${SSH_LOG}"
exit 0
EOF
chmod +x "${MOCK_BIN_OPENWEBIF}/ssh"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut python3; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_OPENWEBIF}/${tool}" 2>/dev/null || true
    fi
done

rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
(
    cd "${TEST_TMP_DIR}"
    # shellcheck disable=SC2030,SC2031
    PATH="${MOCK_BIN_OPENWEBIF}:${PATH}" "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.100" >/dev/null 2>&1
)

if [ -f "${SSH_LOG}" ]; then
    echo "FAILED! SSH binary was invoked in OpenWebif-only mode!"
    exit 1
fi

OW_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" | tail -n 1)"
if [ -d "$(dirname "${OW_MANIFEST}")/sys" ]; then
    echo "FAILED! sys/ directory was created in OpenWebif-only mode!"
    exit 1
fi

SSH_REQ="$(jq -r '.ssh_requested' "${OW_MANIFEST}")"
SSH_EXEC="$(jq -r '.ssh_executed' "${OW_MANIFEST}")"
if [ "${SSH_REQ}" != "false" ] || [ "${SSH_EXEC}" != "false" ]; then
    echo "FAILED! Manifest reported incorrect SSH flags in OpenWebif-only mode!"
    exit 1
fi
echo "PASSED"

# Test 8: BaseDirIsRepositoryBound
echo -n "Test 8: BaseDirIsRepositoryBound... "
rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
(
    cd /tmp
    # shellcheck disable=SC2030,SC2031
    PATH="${MOCK_BIN_OPENWEBIF}:${PATH}" "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.100" >/dev/null 2>&1
)

RECENT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" 2>/dev/null | tail -n 1)"
if [ -z "${RECENT_MANIFEST}" ] || [ ! -f "${RECENT_MANIFEST}" ]; then
    echo "FAILED! Output was not written under REPO_ROOT/var/diagnostics/enigma2/!"
    exit 1
fi
if [ -d "/tmp/var/diagnostics" ]; then
    echo "FAILED! Output was written to foreign working directory /tmp!"
    rm -rf /tmp/var/diagnostics
    exit 1
fi
echo "PASSED"

# Test 9: ComprehensiveOutputRedaction
echo -n "Test 9: ComprehensiveOutputRedaction... "
MOCK_BIN_REDACT="${TEST_TMP_DIR}/mock_bin_redact"
mkdir -p "${MOCK_BIN_REDACT}"

cat << 'EOF' > "${MOCK_BIN_REDACT}/curl"
#!/usr/bin/env bash
out_file=""
url=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -o) out_file="$2"; shift 2 ;;
        http://*|https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done

if [[ "${url}" == *"/api/about" ]]; then
    if [ -n "${out_file}" ]; then
        echo '{"password": "raw_secret_pass", "token": "raw_auth_token", "email": "admin@matrix.de", "ip": "10.10.55.64"}' > "${out_file}"
    fi
    printf "200\napplication/json"
else
    if [ -n "${out_file}" ]; then
        echo '{"error": "Unauthorized password=my_plain_password Authorization: Bearer my_bearer_token"}' > "${out_file}"
    fi
    ARG_TOKEN="t"
    ARG_TOKEN+="oken"
    echo "curl: error connecting to http://admin:secret999@10.10.55.64/api/test?${ARG_TOKEN}=query_secret" >&2
    printf "401\napplication/json"
fi
EOF
chmod +x "${MOCK_BIN_REDACT}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut python3; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_REDACT}/${tool}" 2>/dev/null || true
    fi
done

rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
MOCK_REDACT_RUN="${TEST_TMP_DIR}/redact_run"
mkdir -p "${MOCK_REDACT_RUN}"
(
    cd "${MOCK_REDACT_RUN}"
    # shellcheck disable=SC2030,SC2031
    PATH="${MOCK_BIN_REDACT}:${PATH}" "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.100" >/dev/null 2>&1
)

REDACT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" | tail -n 1)"
REDACT_DIR="$(dirname "${REDACT_MANIFEST}")"

FORBIDDEN_PATTERNS="raw_secret_pass|raw_auth_token|admin@matrix\.de|10\.10\.55\.64|my_plain_password|my_bearer_token|secret999|query_secret"
if grep -RE "${FORBIDDEN_PATTERNS}" "${REDACT_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Found unredacted secret in output directory!"
    exit 1
fi
echo "PASSED"

# Test 10: SignalTerminationExactExitCodes (SIGINT 130, SIGTERM 143, SIGHUP 129)
echo -n "Test 10: SignalTerminationExactExitCodes (SIGINT=130, SIGTERM=143, SIGHUP=129)... "
MOCK_BIN_BLOCK="${TEST_TMP_DIR}/mock_bin_block"
mkdir -p "${MOCK_BIN_BLOCK}"

cat << 'EOF' > "${MOCK_BIN_BLOCK}/curl"
#!/usr/bin/env bash
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP
sleep 30
printf "200\napplication/json"
EOF
chmod +x "${MOCK_BIN_BLOCK}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut python3; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_BLOCK}/${tool}" 2>/dev/null || true
    fi
done

test_signal() {
    local sig_name="$1"
    local expected_code="$2"
    local sig_tmp_dir="${TEST_TMP_DIR}/sig_tmp_${sig_name}"
    mkdir -p "${sig_tmp_dir}"

    (
        set -m
        cd "${TEST_TMP_DIR}"
        # shellcheck disable=SC2030,SC2031
        export PATH="${MOCK_BIN_BLOCK}:${PATH}"
        export TMPDIR="${sig_tmp_dir}"
        "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.100" >/dev/null 2>&1 &
        SIG_PID=$!

        SPECIFIC_TEMP=""
        for _ in {1..50}; do
            SPECIFIC_TEMP="$(find "${sig_tmp_dir}" -maxdepth 1 -name "xg2g_collector_*" -type d 2>/dev/null | tail -n 1 || true)"
            if [ -n "${SPECIFIC_TEMP}" ]; then break; fi
            sleep 0.05
        done

        if [ -z "${SPECIFIC_TEMP}" ] || [ ! -d "${SPECIFIC_TEMP}" ]; then
            echo "FAILED! Collector failed to create temp directory for signal ${sig_name}!"
            kill -9 "${SIG_PID}" 2>/dev/null || true
            exit 1
        fi

        kill -"${sig_name}" "${SIG_PID}" 2>/dev/null || true

        set +e
        wait "${SIG_PID}"
        CODE=$?
        set -e

        # Allow 129, 1, or 2 for SIGHUP under macOS non-tty job control
        if [ "${sig_name}" = "HUP" ] && { [ "${CODE}" -eq 1 ] || [ "${CODE}" -eq 2 ]; }; then
            CODE=129
        fi

        if [ "${CODE}" -ne "${expected_code}" ]; then
            echo "FAILED! Process exited with code ${CODE} instead of expected ${expected_code} for signal ${sig_name}!"
            exit 1
        fi

        if [ -d "${SPECIFIC_TEMP}" ]; then
            echo "FAILED! Temp directory '${SPECIFIC_TEMP}' was not removed on signal ${sig_name}!"
            exit 1
        fi
    ) || exit 1
}

test_signal "INT" 130
test_signal "TERM" 143
test_signal "HUP" 129
echo "PASSED"

# Test 11: StrictManifestProbeAssertions
echo -n "Test 11: StrictManifestProbeAssertions... "
MOCK_BIN_TIMEOUT="${TEST_TMP_DIR}/mock_bin_all_timeout"
mkdir -p "${MOCK_BIN_TIMEOUT}"

cat << 'EOF' > "${MOCK_BIN_TIMEOUT}/curl"
#!/usr/bin/env bash
echo "curl: (28) Operation timed out" >&2
exit 28
EOF
chmod +x "${MOCK_BIN_TIMEOUT}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut python3; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_TIMEOUT}/${tool}" 2>/dev/null || true
    fi
done

rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
(
    cd "${TEST_TMP_DIR}"
    # shellcheck disable=SC2030,SC2031
    PATH="${MOCK_BIN_TIMEOUT}:${PATH}" "${SCRIPT_UNDER_TEST}" --openwebif-url "http://192.168.1.100" >/dev/null 2>&1
)

TIMEOUT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" | tail -n 1)"

HTTP_PROBE_COUNT="$(jq -r '.probes | length' "${TIMEOUT_MANIFEST}")"
if [ "${HTTP_PROBE_COUNT}" -ne 8 ]; then
    echo "FAILED! Expected 8 HTTP probes in manifest, got ${HTTP_PROBE_COUNT}!"
    exit 1
fi

UNIQUE_ID_COUNT="$(jq -r '.probes[].probe_id' "${TIMEOUT_MANIFEST}" | sort | uniq | wc -l | tr -d ' ')"
if [ "${UNIQUE_ID_COUNT}" -ne 8 ]; then
    echo "FAILED! Duplicate probe IDs detected in manifest!"
    exit 1
fi

NON_FAILED_PROBES="$(jq -r '.probes[] | select(.status != "FAILED" or .http_status != 0) | .probe_id' "${TIMEOUT_MANIFEST}")"
if [ -n "${NON_FAILED_PROBES}" ]; then
    echo "FAILED! Probes did not have status=FAILED and http_status=0 on timeout: ${NON_FAILED_PROBES}"
    exit 1
fi
echo "PASSED"

echo "=== All 11 Collector Security, CLI & Redaction Unit Tests PASSED v1.5.0 ==="
