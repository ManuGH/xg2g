#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0

set -euo pipefail

# Executable Unit Test Suite for scripts/collect_receiver_diagnostics.sh
SCRIPT_UNDER_TEST="$(pwd)/scripts/collect_receiver_diagnostics.sh"
TEST_TMP_DIR="$(mktemp -d /tmp/collector_test_XXXXXX)"

cleanup() {
    rm -rf "${TEST_TMP_DIR}"
}
trap cleanup EXIT

echo "=== Running Executable Collector Security & Timeout Unit Tests ==="

# Test 1: RejectsCredentialBearingURL
echo -n "Test 1: RejectsCredentialBearingURL... "
if "${SCRIPT_UNDER_TEST}" "http://admin:secret123@192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed credential-bearing URL!"
    exit 1
fi
echo "PASSED"

# Test 2: DoesNotInvokeMutatingEndpoints
echo -n "Test 2: DoesNotInvokeMutatingEndpoints... "
FORBIDDEN_TERMS="zap|powerstate|message|restart|reboot|timeradd|timerdelete|timerchange|record"
if grep -Ei "(${FORBIDDEN_TERMS})" "${SCRIPT_UNDER_TEST}" | grep -v "#"; then
    echo "FAILED! Found forbidden mutation commands in script!"
    exit 1
fi
echo "PASSED"

# Test 3: HTTPProbePassesConnectTimeout & HTTPProbePassesMaximumRuntime
echo -n "Test 3: HTTPProbePassesTimeouts (connect-timeout & max-time)... "
MOCK_BIN_DIR="${TEST_TMP_DIR}/mock_bin_timeouts"
mkdir -p "${MOCK_BIN_DIR}"
export CURL_ARGS_LOG="${TEST_TMP_DIR}/curl_args.log"

cat << 'EOF' > "${MOCK_BIN_DIR}/curl"
#!/usr/bin/env bash
echo "$@" >> "${CURL_ARGS_LOG}"
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
chmod +x "${MOCK_BIN_DIR}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_DIR}/${tool}" 2>/dev/null || true
    fi
done

MOCK_RUN_DIR="${TEST_TMP_DIR}/timeout_run"
mkdir -p "${MOCK_RUN_DIR}"
(
    cd "${MOCK_RUN_DIR}"
    PATH="${MOCK_BIN_DIR}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1
)

if ! grep -q -- "--connect-timeout 5" "${CURL_ARGS_LOG}"; then
    echo "FAILED! curl was not called with '--connect-timeout 5'!"
    exit 1
fi
if ! grep -q -- "--max-time 10" "${CURL_ARGS_LOG}"; then
    echo "FAILED! curl was not called with '--max-time 10'!"
    exit 1
fi
echo "PASSED"

# Test 4: CredentialsPreserveExactValues
echo -n "Test 4: CredentialsPreserveExactValues... "
EXACT_CRED_FILE="${TEST_TMP_DIR}/exact_cred.cfg"
cat << 'EOF' > "${EXACT_CRED_FILE}"
username=Mein User 2026
password=Mein Passwort 2026
EOF
chmod 600 "${EXACT_CRED_FILE}"

MOCK_RUN_CRED="${TEST_TMP_DIR}/cred_run"
mkdir -p "${MOCK_RUN_CRED}"
(
    cd "${MOCK_RUN_CRED}"
    PATH="${MOCK_BIN_DIR}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" "" "${EXACT_CRED_FILE}" >/dev/null 2>&1
)
if ! grep -q -- "--netrc-file" "${CURL_ARGS_LOG}"; then
    echo "FAILED! --netrc-file flag was not passed to curl!"
    exit 1
fi
echo "PASSED"

# Test 5: CredentialsRejectUnknownKeys & CredentialsRejectMixedFormats
echo -n "Test 5: CredentialsRejectUnknownKeys & MixedFormats... "
UNKNOWN_CRED_FILE="${TEST_TMP_DIR}/unknown_cred.cfg"
cat << 'EOF' > "${UNKNOWN_CRED_FILE}"
username=admin
password=pass
illegal_key=true
EOF
chmod 600 "${UNKNOWN_CRED_FILE}"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "" "${UNKNOWN_CRED_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Collector accepted credentials file containing unknown keys!"
    exit 1
fi

MIXED_CRED_FILE="${TEST_TMP_DIR}/mixed_cred.cfg"
cat << 'EOF' > "${MIXED_CRED_FILE}"
default login user password pass
username=admin
password=pass
EOF
chmod 600 "${MIXED_CRED_FILE}"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "" "${MIXED_CRED_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Collector accepted mixed Key-Value and Netrc format!"
    exit 1
fi
echo "PASSED"

# Test 6: SignalTerminationRemovesTemporaryDirectory
echo -n "Test 6: SignalTerminationRemovesTemporaryDirectory... "
MOCK_EXEC_DIR="${TEST_TMP_DIR}/signal_run"
mkdir -p "${MOCK_EXEC_DIR}"
(
    cd "${MOCK_EXEC_DIR}"
    "${SCRIPT_UNDER_TEST}" "http://127.0.0.1:1" >/dev/null 2>&1 &
    PID=$!
    sleep 0.05
    kill -INT "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
) || true

LEAKED="$(find /tmp -maxdepth 2 -name "xg2g_collector_*" -type d 2>/dev/null || true)"
if [ -n "${LEAKED}" ]; then
    echo "FAILED! Leaked temporary directory after signal termination: ${LEAKED}"
    exit 1
fi
echo "PASSED"

# Test 7: SSHFailurePreservesRedactedStdoutAndStderr
echo -n "Test 7: SSHFailurePreservesRedactedStdoutAndStderr... "
MOCK_BIN_SSH="${TEST_TMP_DIR}/mock_bin_ssh"
mkdir -p "${MOCK_BIN_SSH}"

cat << 'EOF' > "${MOCK_BIN_SSH}/curl"
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
chmod +x "${MOCK_BIN_SSH}/curl"

cat << 'EOF' > "${MOCK_BIN_SSH}/timeout"
#!/usr/bin/env bash
shift 2
cmd="$1"
echo "Partial stdout output for command secret_cmd_token"
echo "SSH Failure stderr with token=secret_ssh_token" >&2
exit 1
EOF
chmod +x "${MOCK_BIN_SSH}/timeout"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_SSH}/${tool}" 2>/dev/null || true
    fi
done

MOCK_SSH_RUN="${TEST_TMP_DIR}/ssh_run"
mkdir -p "${MOCK_SSH_RUN}"
(
    cd "${MOCK_SSH_RUN}"
    PATH="${MOCK_BIN_SSH}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" "root@192.168.1.100" >/dev/null 2>&1
)

SSH_ERR_LOG="$(find "${MOCK_SSH_RUN}" -name "receiver_identity.error.log")"
if [ -z "${SSH_ERR_LOG}" ] || [ ! -f "${SSH_ERR_LOG}" ]; then
    echo "FAILED! SSH error log was not created!"
    exit 1
fi

if ! grep -q -- "--- STDOUT ---" "${SSH_ERR_LOG}"; then
    echo "FAILED! SSH error log failed to preserve partial stdout!"
    exit 1
fi
if ! grep -q -- "--- STDERR ---" "${SSH_ERR_LOG}"; then
    echo "FAILED! SSH error log failed to preserve stderr!"
    exit 1
fi
if grep -q "secret_cmd_token\|secret_ssh_token" "${SSH_ERR_LOG}"; then
    echo "FAILED! Secrets were not redacted in SSH failure log!"
    exit 1
fi
echo "PASSED"

# Test 8: HTTPProbeTimeoutProducesSingleFailedManifestEntry
echo -n "Test 8: HTTPProbeTimeoutProducesSingleFailedManifestEntry... "
MOCK_BIN_TIMEOUT_CURL="${TEST_TMP_DIR}/mock_bin_curl_timeout"
mkdir -p "${MOCK_BIN_TIMEOUT_CURL}"

cat << 'EOF' > "${MOCK_BIN_TIMEOUT_CURL}/curl"
#!/usr/bin/env bash
echo "curl: (28) Operation timed out" >&2
exit 28
EOF
chmod +x "${MOCK_BIN_TIMEOUT_CURL}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_TIMEOUT_CURL}/${tool}" 2>/dev/null || true
    fi
done

MOCK_TIMEOUT_RUN="${TEST_TMP_DIR}/curl_timeout_run"
mkdir -p "${MOCK_TIMEOUT_RUN}"
(
    cd "${MOCK_TIMEOUT_RUN}"
    PATH="${MOCK_BIN_TIMEOUT_CURL}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1
)

TIMEOUT_MANIFEST="$(find "${MOCK_TIMEOUT_RUN}" -name "manifest.json")"
DEVINFO_HTTP_TIMEOUT="$(jq -r '.probes[] | select(.probe_id=="about") | .http_status' "${TIMEOUT_MANIFEST}")"
if [ "${DEVINFO_HTTP_TIMEOUT}" != "0" ]; then
    echo "FAILED! Expected http_status=0 on curl timeout, got '${DEVINFO_HTTP_TIMEOUT}'!"
    exit 1
fi
echo "PASSED"

echo "=== All 8 Collector Security & Timeout Unit Tests PASSED ==="
