#!/usr/bin/env bash
set -euo pipefail

# Guardrail for the v3 package decomposition: block fan-out regressions.
MAX_V3_FANOUT="${MAX_V3_FANOUT:-77}"
ACTUAL_V3_FANOUT="$(go list -f '{{len .Imports}}' ./internal/control/http/v3)"

if [ "${ACTUAL_V3_FANOUT}" -gt "${MAX_V3_FANOUT}" ]; then
  echo "FAIL: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
  exit 1
fi

echo "OK: internal/control/http/v3 fan-out is ${ACTUAL_V3_FANOUT} (max: ${MAX_V3_FANOUT})"
