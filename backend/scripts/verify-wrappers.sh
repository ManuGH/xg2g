#!/usr/bin/env bash
set -e

# verify-wrappers.sh
# Verifies that FFmpeg/FFprobe wrappers respect FFMPEG_HOME env var.
# This serves as the CI gate for PR B.

SCRIPT_DIR="$(dirname "$(readlink -f "$0")")"
FFMPEG_WRAPPER="${SCRIPT_DIR}/ffmpeg-wrapper.sh"
FFPROBE_WRAPPER="${SCRIPT_DIR}/ffprobe-wrapper.sh"

echo "=== Verifying Hermetic Wrappers ==="

# 1. Mock Environment
MOCK_HOME="/tmp/mock-ffmpeg-home"
mkdir -p "${MOCK_HOME}/bin" "${MOCK_HOME}/lib"
touch "${MOCK_HOME}/bin/ffmpeg" "${MOCK_HOME}/bin/ffprobe"
chmod +x "${MOCK_HOME}/bin/ffmpeg" "${MOCK_HOME}/bin/ffprobe"

# 2. Test Cases
fail=0

check_env() {
    local wrapper="$1"
    local tool="$2"
    local expected_path="$3"
    
    # We use --print-realpath which is supported by the wrappers to check resolution
    # without actually running the binary (which might fail due to missing libs).
    local output
    output=$(FFMPEG_HOME="${MOCK_HOME}" "${wrapper}" --print-realpath)
    
    if [[ "${output}" == "${expected_path}" ]]; then
        echo "✅ ${tool}: Respects FFMPEG_HOME (Required: ${expected_path}, Got: ${output})"
    else
        echo "❌ ${tool}: FAILED to respect FFMPEG_HOME (Required: ${expected_path}, Got: ${output})"
        fail=1
    fi
}

check_default() {
    local wrapper="$1"
    local tool="$2"
    local expected_path="$3"

    # Unset FFMPEG_HOME to test default
    local output
    output=$(unset FFMPEG_HOME; "${wrapper}" --print-realpath)

    if [[ "${output}" == "${expected_path}" ]]; then
         echo "✅ ${tool}: Default path correct (Required: ${expected_path}, Got: ${output})"
    else
         echo "❌ ${tool}: FAILED default path (Required: ${expected_path}, Got: ${output})"
         fail=1
    fi
}

# Run Checks
# Test 1: Explicit FFMPEG_HOME
check_env "${FFMPEG_WRAPPER}" "ffmpeg" "${MOCK_HOME}/bin/ffmpeg"
check_env "${FFPROBE_WRAPPER}" "ffprobe" "${MOCK_HOME}/bin/ffprobe"

# Test 2: Default (should be /opt/ffmpeg/bin/...)
check_default "${FFMPEG_WRAPPER}" "ffmpeg" "/opt/ffmpeg/bin/ffmpeg"
check_default "${FFPROBE_WRAPPER}" "ffprobe" "/opt/ffmpeg/bin/ffprobe"

# Cleanup
rm -rf "${MOCK_HOME}"

if [[ $fail -eq 0 ]]; then
    echo "=== Verification Passed ==="
    exit 0
else
    echo "=== Verification Failed ==="
    exit 1
fi
