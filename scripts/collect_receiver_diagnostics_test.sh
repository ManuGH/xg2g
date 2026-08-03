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

echo "=== Running Strict Collector Security, SSH & Redaction Unit Tests ==="

# Test 1: RejectsCredentialBearingURL
echo -n "Test 1: RejectsCredentialBearingURL... "
if "${SCRIPT_UNDER_TEST}" "http://admin:secret123@192.168.1.50" >/dev/null 2>&1; then
    echo "FAILED! Script allowed credential-bearing URL!"
    exit 1
fi
echo "PASSED"

# Test 2: SSHTargetRejectsOptionInjection
echo -n "Test 2: SSHTargetRejectsOptionInjection... "
HACK_FILE="${TEST_TMP_DIR}/hacked"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "-oProxyCommand=touch ${HACK_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed SSH option injection starting with -o!"
    exit 1
fi
if [ -f "${HACK_FILE}" ]; then
    echo "FAILED! Executed injected ProxyCommand!"
    exit 1
fi

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "user host" >/dev/null 2>&1; then
    echo "FAILED! Script allowed SSH target containing whitespace!"
    exit 1
fi
if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "-oStrictHostKeyChecking=no" >/dev/null 2>&1; then
    echo "FAILED! Script allowed SSH target starting with dash!"
    exit 1
fi
echo "PASSED"

# Test 3: CredentialsRejectSpacesAndQuotes
echo -n "Test 3: CredentialsRejectSpacesAndQuotes... "
SPACE_CRED_FILE="${TEST_TMP_DIR}/space_cred.cfg"
cat << 'EOF' > "${SPACE_CRED_FILE}"
username=Mein User 2026
password=Pass123
EOF
chmod 600 "${SPACE_CRED_FILE}"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "" "${SPACE_CRED_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed username containing spaces!"
    exit 1
fi

QUOTE_CRED_FILE="${TEST_TMP_DIR}/quote_cred.cfg"
cat << 'EOF' > "${QUOTE_CRED_FILE}"
username=admin
password=pass"word
EOF
chmod 600 "${QUOTE_CRED_FILE}"

if "${SCRIPT_UNDER_TEST}" "192.168.1.50" "" "${QUOTE_CRED_FILE}" >/dev/null 2>&1; then
    echo "FAILED! Script allowed password containing double-quotes!"
    exit 1
fi
echo "PASSED"

# Test 4: BaseDirIsRepositoryBound
echo -n "Test 4: BaseDirIsRepositoryBound... "
MOCK_BIN_BOUND="${TEST_TMP_DIR}/mock_bin_bound"
mkdir -p "${MOCK_BIN_BOUND}"

cat << 'EOF' > "${MOCK_BIN_BOUND}/curl"
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
chmod +x "${MOCK_BIN_BOUND}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_BOUND}/${tool}" 2>/dev/null || true
    fi
done

# Run collector from /tmp (foreign directory)
(
    cd /tmp
    PATH="${MOCK_BIN_BOUND}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1
)

# Verify output directory is created in REPO_ROOT/var/diagnostics/enigma2/, NOT in /tmp/var/diagnostics/
RECENT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" 2>/dev/null | tail -n 1)"
if [ -z "${RECENT_MANIFEST}" ] || [ ! -f "${RECENT_MANIFEST}" ]; then
    echo "FAILED! Output was not written under REPO_ROOT/var/diagnostics/enigma2/!"
    exit 1
fi
if [ -d "/tmp/var/diagnostics" ]; then
    echo "FAILED! Output was written to current working directory /tmp instead of REPO_ROOT!"
    rm -rf /tmp/var/diagnostics
    exit 1
fi
echo "PASSED"

# Test 5: ComprehensiveOutputRedaction
echo -n "Test 5: ComprehensiveOutputRedaction... "
MOCK_BIN_REDACT="${TEST_TMP_DIR}/mock_bin_redact"
mkdir -p "${MOCK_BIN_REDACT}"

cat << 'EOF' > "${MOCK_BIN_REDACT}/curl"
#!/usr/bin/env bash
out_file=""
err_file=""
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

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
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
    PATH="${MOCK_BIN_REDACT}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1
)

REDACT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" | tail -n 1)"
REDACT_DIR="$(dirname "${REDACT_MANIFEST}")"

# Assert no unredacted secrets anywhere in output directory
FORBIDDEN_PATTERNS="raw_secret_pass|raw_auth_token|admin@matrix\.de|10\.10\.55\.64|my_plain_password|my_bearer_token|secret999|query_secret"
if grep -RE "${FORBIDDEN_PATTERNS}" "${REDACT_DIR}" >/dev/null 2>&1; then
    echo "FAILED! Found unredacted secret in output directory:"
    grep -RE "${FORBIDDEN_PATTERNS}" "${REDACT_DIR}"
    exit 1
fi

# Assert redaction markers are present
if ! grep -R "\[REDACTED\]\|\[REDACTED_IP\]\|\[REDACTED_EMAIL\]\|\[REDACTED_AUTH\]" "${REDACT_DIR}" >/dev/null 2>&1; then
    echo "FAILED! No redaction markers found in output directory!"
    exit 1
fi
echo "PASSED"

# Test 6: SignalTerminationIsDeterministic
echo -n "Test 6: SignalTerminationIsDeterministic... "
MOCK_BIN_BLOCK="${TEST_TMP_DIR}/mock_bin_block"
mkdir -p "${MOCK_BIN_BLOCK}"
SIG_TMP_DIR="${TEST_TMP_DIR}/sig_tmp"
mkdir -p "${SIG_TMP_DIR}"

cat << 'EOF' > "${MOCK_BIN_BLOCK}/curl"
#!/usr/bin/env bash
sleep 30
printf "200\napplication/json"
EOF
chmod +x "${MOCK_BIN_BLOCK}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_BLOCK}/${tool}" 2>/dev/null || true
    fi
done

MOCK_SIG_RUN="${TEST_TMP_DIR}/sig_run"
mkdir -p "${MOCK_SIG_RUN}"

(
    cd "${MOCK_SIG_RUN}"
    # shellcheck disable=SC2030,SC2031
    export PATH="${MOCK_BIN_BLOCK}:${PATH}"
    export TMPDIR="${SIG_TMP_DIR}"
    "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1 &
    SIG_PID=$!

    # Wait until collector process creates its specific temp directory
    SPECIFIC_TEMP=""
    for _ in {1..50}; do
        SPECIFIC_TEMP="$(find "${SIG_TMP_DIR}" -maxdepth 1 -name "xg2g_collector_*" -type d 2>/dev/null | tail -n 1 || true)"
        if [ -n "${SPECIFIC_TEMP}" ]; then break; fi
        sleep 0.05
    done

    if [ -z "${SPECIFIC_TEMP}" ] || [ ! -d "${SPECIFIC_TEMP}" ]; then
        echo "FAILED! Collector failed to create temp directory!"
        kill -9 "${SIG_PID}" 2>/dev/null || true
        exit 1
    fi

    # Send SIGINT to process
    kill -INT "${SIG_PID}" 2>/dev/null || true

    set +e
    wait "${SIG_PID}"
    CODE=$?
    set -e

    if [ "${CODE}" -eq 0 ]; then
        echo "FAILED! Process exited with code 0 after SIGINT!"
        exit 1
    fi

    if [ -d "${SPECIFIC_TEMP}" ]; then
        echo "FAILED! Specific temporary directory '${SPECIFIC_TEMP}' was not removed on SIGINT!"
        exit 1
    fi
) || exit 1
echo "PASSED"

# Test 7: StrictManifestProbeAssertions
echo -n "Test 7: StrictManifestProbeAssertions... "
MOCK_BIN_TIMEOUT="${TEST_TMP_DIR}/mock_bin_all_timeout"
mkdir -p "${MOCK_BIN_TIMEOUT}"

cat << 'EOF' > "${MOCK_BIN_TIMEOUT}/curl"
#!/usr/bin/env bash
echo "curl: (28) Operation timed out" >&2
exit 28
EOF
chmod +x "${MOCK_BIN_TIMEOUT}/curl"

for tool in sh bash grep sed awk cat date mkdir rm stat shasum sha256sum jq mktemp tr head cut; do
    TOOL_PATH="$(which ${tool} 2>/dev/null || true)"
    if [ -n "${TOOL_PATH}" ]; then
        ln -s "${TOOL_PATH}" "${MOCK_BIN_TIMEOUT}/${tool}" 2>/dev/null || true
    fi
done

rm -rf "${REPO_ROOT}/var/diagnostics/enigma2"
MOCK_ALL_TIMEOUT_RUN="${TEST_TMP_DIR}/all_timeout_run"
mkdir -p "${MOCK_ALL_TIMEOUT_RUN}"
(
    cd "${MOCK_ALL_TIMEOUT_RUN}"
    # shellcheck disable=SC2030,SC2031
    PATH="${MOCK_BIN_TIMEOUT}:${PATH}" "${SCRIPT_UNDER_TEST}" "192.168.1.100" >/dev/null 2>&1
)

TIMEOUT_MANIFEST="$(find "${REPO_ROOT}/var/diagnostics/enigma2" -name "manifest.json" | tail -n 1)"

# Assert 7.1: Exactly 8 HTTP probes in manifest
HTTP_PROBE_COUNT="$(jq -r '.probes | length' "${TIMEOUT_MANIFEST}")"
if [ "${HTTP_PROBE_COUNT}" -ne 8 ]; then
    echo "FAILED! Expected 8 HTTP probes in manifest, got ${HTTP_PROBE_COUNT}!"
    exit 1
fi

# Assert 7.2: Exactly 8 unique probe IDs (no duplicates)
UNIQUE_ID_COUNT="$(jq -r '.probes[].probe_id' "${TIMEOUT_MANIFEST}" | sort | uniq | wc -l | tr -d ' ')"
if [ "${UNIQUE_ID_COUNT}" -ne 8 ]; then
    echo "FAILED! Duplicate probe IDs detected in manifest!"
    exit 1
fi

# Assert 7.3: Every probe in timeout run has status=="FAILED" and http_status==0
NON_FAILED_PROBES="$(jq -r '.probes[] | select(.status != "FAILED" or .http_status != 0) | .probe_id' "${TIMEOUT_MANIFEST}")"
if [ -n "${NON_FAILED_PROBES}" ]; then
    echo "FAILED! Probes did not have status=FAILED and http_status=0 on timeout: ${NON_FAILED_PROBES}"
    exit 1
fi
echo "PASSED"

echo "=== All 7 Strict Collector Security & Redaction Unit Tests PASSED ==="
