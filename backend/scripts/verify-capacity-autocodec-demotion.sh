#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="${REPO_ROOT:-$(git rev-parse --show-toplevel)}"
BACKEND_ROOT="${REPO_ROOT}/backend"

run() {
  echo "==> $*"
  "$@"
}

cd "${BACKEND_ROOT}"

run go test ./internal/control/http/v3/autocodec -run 'TestCapacityHarness_' -count=1
run go test ./internal/control/http/v3/recordings -run 'Test(PickPlaybackInfoAutoProfile_UsesAV1OnlyOnHealthyHost|AlignAutoCodecDecision_PersistsNeutralSelectionTrace)$' -count=1
run go test ./internal/control/http/v3/intents -run 'Test(Service_ProcessIntent_StartUsesHostRuntimeToDemoteAV1ToHEVC|Service_ProcessIntent_StartUsesEncodeOnlyForIOSNativeHEVC)$' -count=1

echo "✅ capacity/performance auto-codec harness passed"
