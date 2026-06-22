#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
COMPOSE_HELPER="${XG2G_DEV_COMPOSE_HELPER:-${SCRIPT_DIR}/compose-xg2g.sh}"
ENV_FILE="${XG2G_DEV_ENV_FILE:-${REPO_ROOT}/.env}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  echo "usage: backend/scripts/dev-compose.sh <base|vaapi|nvidia> <compose-arguments...>" >&2
  exit 64
}

runtime="${1:-}"
[[ -n "${runtime}" ]] || usage
shift
[[ "$#" -gt 0 ]] || usage

case "${runtime}" in
  base)
    compose_files="docker-compose.yml:../docker-compose.dev.yml"
    ;;
  vaapi)
    compose_files="docker-compose.yml:../docker-compose.dev.yml:docker-compose.gpu.yml"
    ;;
  nvidia)
    compose_files="docker-compose.yml:../docker-compose.dev.yml:docker-compose.nvidia.yml"
    ;;
  *)
    fail "unknown development runtime '${runtime}'; expected base, vaapi, or nvidia"
    ;;
esac

[[ -x "${COMPOSE_HELPER}" ]] || fail "compose helper is not executable: ${COMPOSE_HELPER}"
[[ -f "${ENV_FILE}" ]] || fail "missing ${ENV_FILE} (run: make install)"

exec env \
  XG2G_COMPOSE_ROOT="${REPO_ROOT}/deploy" \
  XG2G_COMPOSE_PROJECT="xg2g-dev" \
  XG2G_ENV_FILE="${ENV_FILE}" \
  XG2G_COMPOSE_FILES_LOCKED=1 \
  COMPOSE_FILE="${compose_files}" \
  "${COMPOSE_HELPER}" "$@"
