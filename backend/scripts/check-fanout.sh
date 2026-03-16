#!/usr/bin/env bash
set -euo pipefail

# Guardrail for the v3 package decomposition: block fan-out regressions.
# Baseline 79 reflects intentional v3 hardening imports:
# - internal/control/http/v3/auth for strict decision-token verification
# - internal/pipeline/hardware for hwaccel availability enforcement
SCRIPT_DIR="$(cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/../.." && pwd)"

cd "${REPO_ROOT}/backend"

MAX_V3_FANOUT="${MAX_V3_FANOUT:-79}"
ACTUAL_V3_FANOUT="$(go list -f '{{len .Imports}}' ./internal/control/http/v3)"

if [ "${ACTUAL_V3_FANOUT}" -gt "${MAX_V3_FANOUT}" ]; then
  echo "FAIL: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
  exit 1
fi

echo "OK: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
