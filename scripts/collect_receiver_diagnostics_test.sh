#!/usr/bin/env bash
# Copyright (c) 2025 ManuGH
# Licensed under the PolyForm Noncommercial License 1.0.0

set -euo pipefail

# Tests for scripts/collect_receiver_diagnostics.sh
SCRIPT_UNDER_TEST="$(pwd)/scripts/collect_receiver_diagnostics.sh"

echo "=== Running Passive Collector Safety & Security Unit Tests ==="

# Test 1: Verify script does NOT contain any forbidden mutation endpoints
echo -n "Test 1: DoesNotInvokeZapTimerRecordingOrRestartEndpoints... "
FORBIDDEN_TERMS="zap|powerstate|message|restart|reboot|timeradd|timerdelete|timerchange|record"
if grep -Ei "(${FORBIDDEN_TERMS})" "${SCRIPT_UNDER_TEST}" | grep -v "#"; then
    echo "FAILED! Found forbidden mutation commands in script!"
    exit 1
fi
echo "PASSED"

# Test 2: Verify output path NEVER writes into docs/
echo -n "Test 2: DoesNotWriteIntoDocs... "
if grep -F "docs/" "${SCRIPT_UNDER_TEST}" | grep -v "#"; then
    echo "FAILED! Found hardcoded output path targeting docs/!"
    exit 1
fi
echo "PASSED"

# Test 3: Verify bounded HTTP timeouts
echo -n "Test 3: HasBoundedHTTPTimeouts... "
if ! grep -q "\-\-connect-timeout" "${SCRIPT_UNDER_TEST}" || ! grep -q "\-\-max-time" "${SCRIPT_UNDER_TEST}"; then
    echo "FAILED! Missing bounded HTTP timeout flags (--connect-timeout / --max-time)!"
    exit 1
fi
echo "PASSED"

# Test 4: Verify credentials redaction helper exists
echo -n "Test 4: RedactsCredentials... "
if ! grep -q "redact_content" "${SCRIPT_UNDER_TEST}"; then
    echo "FAILED! Missing credential redaction filter!"
    exit 1
fi
echo "PASSED"

# Test 5: Verify strict shell flags set -euo pipefail and umask 077
echo -n "Test 5: StrictShellFlagsAndUmask... "
if ! grep -q "set -euo pipefail" "${SCRIPT_UNDER_TEST}" || ! grep -q "umask 077" "${SCRIPT_UNDER_TEST}"; then
    echo "FAILED! Missing set -euo pipefail or umask 077!"
    exit 1
fi
echo "PASSED"

# Test 6: Verify fails without explicit target argument (Usage check)
echo -n "Test 6: FailsWithoutExplicitTargetArgument... "
if "${SCRIPT_UNDER_TEST}" >/dev/null 2>&1; then
    echo "FAILED! Script executed without target argument! Expected non-zero exit code."
    exit 1
fi
echo "PASSED"

# Test 7: Verify NO hardcoded fallback IP addresses exist in script
echo -n "Test 7: NoHardcodedDefaultIP... "
if grep -E '10\.10\.55\.[0-9]+' "${SCRIPT_UNDER_TEST}" | grep -v "#"; then
    echo "FAILED! Found hardcoded 10.10.55.x IP address in collector script!"
    exit 1
fi
echo "PASSED"

echo "=== All Passive Collector Safety Tests PASSED ==="
