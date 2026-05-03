#!/usr/bin/env bash
set -euo pipefail

# Guardrail for the v3 package decomposition: block fan-out regressions.
# Baseline 101 reflects the current intentional v3 composition:
# - original 79-import hardening baseline (strict JWT verification + hwaccel enforcement)
# - internal/admission for receiver/session admission state tracking
# - internal/control/middleware + net for trusted-proxy HTTPS enforcement on session exchange
# - internal/domain/playbackprofile for playback/runtime profile projection
# - internal/control/http/v3/sessions for extracted session read/debug processors
# - internal/problemcode for structured RFC7807 problem mappings
# - v3.4.8–v3.4.9: playback profile integration, recordings capabilities/runtimepolicy,
#   capreg, vod/preflight, channels, control/playback, control/read, platform/net,
#   normalize, m3u, pipeline api/bus, recordings/artifacts, and stdlib additions
#   (crypto/rand, syscall, runtime/debug)
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

cd "${REPO_ROOT}/backend"

MAX_V3_FANOUT="${MAX_V3_FANOUT:-101}"
ACTUAL_IMPORTS_RAW="$(go list -f '{{join .Imports "\n"}}' ./internal/control/http/v3 | sort)"
mapfile -t ACTUAL_IMPORTS <<< "${ACTUAL_IMPORTS_RAW}"
ACTUAL_V3_FANOUT="${#ACTUAL_IMPORTS[@]}"

if [ "${ACTUAL_V3_FANOUT}" -gt "${MAX_V3_FANOUT}" ]; then
  echo "FAIL: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
  printf 'Current imports:\n'
  printf '  %s\n' "${ACTUAL_IMPORTS[@]}"
  exit 1
fi

echo "OK: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
