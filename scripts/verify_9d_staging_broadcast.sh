#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-xg2g-dev}"
REMOTE_BUILD_ROOT="${XG2G_DEPLOY_BUILD_ROOT:-/srv/xg2g-build}"
RECORDING_PATH="${XG2G_TEST_RECORDING:-${1:-}}"
PROGRAM_ID="${XG2G_TEST_PROGRAM:-${2:-0}}"

if [[ -z "${RECORDING_PATH}" ]]; then
  echo "ERROR: Recording path must be provided via XG2G_TEST_RECORDING or as first argument." >&2
  echo "Usage: XG2G_TEST_RECORDING=/path/to/stream.ts [XG2G_TEST_PROGRAM=id] $0" >&2
  echo "   or: $0 /path/to/stream.ts [program_id]" >&2
  exit 1
fi

echo "Running 9d Broadcast & Production-Chunk Acceptance Verification on ${REMOTE_HOST}..."
# shellcheck disable=SC2046
ssh "${REMOTE_HOST}" "bash -s -- $(printf '%q ' "${REMOTE_BUILD_ROOT}" "${RECORDING_PATH}" "${PROGRAM_ID}")" <<'REMOTE'
set -euo pipefail
build_root="$1"
recording="$2"
program="$3"

cd "${build_root}/backend"
/usr/local/go/bin/go run ./cmd/tools/verify-9d-broadcast \
  -recording "${recording}" \
  -core-bin "/usr/local/bin/xg2g-media-core" \
  -chunk-size 65536 \
  -max-mb 10 \
  -program "${program}"
REMOTE
