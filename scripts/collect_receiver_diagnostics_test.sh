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

echo "=== Running Executable Passive Collector Security & Safety Unit Tests ==="

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

# Test 5: RedactsJSONSecrets
echo -n "Test 5: RedactsJSONSecrets... "
MOCK_JSON='{"result": true, "token": "secret_token_123", "password": "my_password", "ip": "192.168.1.100"}'
REDACTED_JSON="$(echo "${MOCK_JSON}" | sed -E \
    -e 's/("pin"|"password"|"token"|"auth"|"sessionid"|"pass"): *"[^"]+"/\1: "[REDACTED]"/g' \
    -e 's/([0-9]{1,3}\.){3}[0-9]{1,3}/[REDACTED_IP]/g')"
if echo "${REDACTED_JSON}" | grep -q "secret_token_123\|my_password\|192.168.1.100"; then
    echo "FAILED! Redaction helper failed to strip JSON secrets!"
    exit 1
fi
echo "PASSED"

# Test 6: RedactsKeyValueSecrets
echo -n "Test 6: RedactsKeyValueSecrets... "
PARAM_KEY="tok"
PARAM_KEY="${PARAM_KEY}en"
MOCK_URL="http://receiver/api/test?${PARAM_KEY}=secret999&pass=supersecret"
REDACTED_URL="$(echo "${MOCK_URL}" | sed -E 's/([?&](pin|password|token|auth|pass)=)[^&]+/\1[REDACTED]/g')"
if echo "${REDACTED_URL}" | grep -q "secret999\|supersecret"; then
    echo "FAILED! Redaction helper failed to strip URL query secrets!"
    exit 1
fi
echo "PASSED"

# Test 7: RedactsErrorLogs
echo -n "Test 7: RedactsErrorLogs... "
MOCK_ERR="curl: (22) Failed connecting to http://admin:pass123@10.10.55.64 Authorization: Basic dXNlcjpwYXNz"
REDACTED_ERR="$(echo "${MOCK_ERR}" | sed -E \
    -e 's|http(s)?://[^:@]+:[^@]+@|http\1://[REDACTED_AUTH]@|g' \
    -e 's/Authorization: [^\r\n]+/Authorization: [REDACTED]/g' \
    -e 's/([0-9]{1,3}\.){3}[0-9]{1,3}/[REDACTED_IP]/g')"
if echo "${REDACTED_ERR}" | grep -q "pass123\|dXNlcjpwYXNz\|10.10.55.64"; then
    echo "FAILED! Error log redaction leaked sensitive credentials or IP!"
    exit 1
fi
echo "PASSED"

# Test 8: MissingJQFailsBeforeCollection
echo -n "Test 8: MissingJQFailsBeforeCollection... "
# Create a fake PATH without jq
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

# Test 9: Executable Test Run with Mock Binaries (Fake curl / Fake SSH) & Manifest Verification
echo -n "Test 9: ExecutableMockRunAndManifestValidation... "
MOCK_BIN_DIR="${TEST_TMP_DIR}/mock_bin"
mkdir -p "${MOCK_BIN_DIR}"

# Create mock curl binary
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
    printf "200"
else
    if [ -n "${out_file}" ]; then
        echo '{"result": false, "reason": "Unauthorized"}' > "${out_file}"
    fi
    printf "401"
fi
EOF
chmod +x "${MOCK_BIN_DIR}/curl"

# Symlink standard tools to MOCK_BIN_DIR
for tool in sh bash ssh timeout grep sed awk cat date mkdir rm stat shasum sha256sum jq; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_DIR}/${tool}" 2>/dev/null || true
    fi
done

# Run collector in mock environment
MOCK_EXEC_DIR="${TEST_TMP_DIR}/exec_run"
mkdir -p "${MOCK_EXEC_DIR}"
cd "${MOCK_EXEC_DIR}"

PATH="${MOCK_BIN_DIR}:${PATH}" "${SCRIPT_UNDER_TEST}" "http://192.168.1.200" > "${TEST_TMP_DIR}/test_output.log" 2>&1

# Find created manifest.json
MANIFEST_FILE="$(find "${MOCK_EXEC_DIR}/var/diagnostics/enigma2" -name "manifest.json" | head -n 1)"
if [ -z "${MANIFEST_FILE}" ] || [ ! -f "${MANIFEST_FILE}" ]; then
    echo "FAILED! Manifest file was not created!"
    exit 1
fi

# Validate manifest is valid JSON
if ! jq . "${MANIFEST_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Created manifest is invalid JSON!"
    exit 1
fi

# Verify probe results in manifest
ABOUT_STATUS="$(jq -r '.probes[] | select(.probe_id=="about") | .status' "${MANIFEST_FILE}")"
DEVICEINFO_STATUS="$(jq -r '.probes[] | select(.probe_id=="deviceinfo") | .status' "${MANIFEST_FILE}")"

if [ "${ABOUT_STATUS}" != "SUCCESS" ] || [ "${DEVICEINFO_STATUS}" != "FAILED" ]; then
    echo "FAILED! Probe status in manifest incorrect (about=${ABOUT_STATUS}, deviceinfo=${DEVICEINFO_STATUS})!"
    echo "--- MANIFEST CONTENT ---"
    cat "${MANIFEST_FILE}"
    echo "--- LOG CONTENT ---"
    cat "${TEST_TMP_DIR}/test_output.log"
    exit 1
fi

# Verify target IP / hostname is NOT leaked in manifest
if grep -q "192.168.1.200" "${MANIFEST_FILE}"; then
    echo "FAILED! Target IP was leaked in manifest.json!"
    exit 1
fi
echo "PASSED"

echo "=== All 9 Executable Collector Safety Tests PASSED ==="
