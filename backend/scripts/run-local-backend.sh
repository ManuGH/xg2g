#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
BACKEND_ROOT="${REPO_ROOT}/backend"
ENV_FILE="${REPO_ROOT}/.env"
ENV_EXAMPLE="${REPO_ROOT}/.env.example"
OUTPUT_DIR="${REPO_ROOT}/bin"
LIB_ENV="${SCRIPT_DIR}/lib/env.sh"
UI_MODE=0

source "${LIB_ENV}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

ensure_env_file() {
  if [[ -f "${ENV_FILE}" ]]; then
    return 0
  fi

  [[ -f "${ENV_EXAMPLE}" ]] || fail "missing ${ENV_EXAMPLE}"
  cp "${ENV_EXAMPLE}" "${ENV_FILE}"
}

build_ldflags() {
  local version commit_hash build_date

  version="$(git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null || echo dev)"
  commit_hash="$(git -C "${REPO_ROOT}" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
  build_date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

  printf '%s' "-s -w -buildid= -X github.com/ManuGH/xg2g/internal/version.Version=${version} -X github.com/ManuGH/xg2g/internal/version.Commit=${commit_hash} -X github.com/ManuGH/xg2g/internal/version.Date=${build_date}"
}

main() {
  local output_bin ldflags go_bin
  local -a cmd

  if [[ "${1:-}" == "--ui" ]]; then
    UI_MODE=1
    shift
  fi
  [[ "$#" -eq 0 ]] || fail "unexpected arguments: $*"

  mkdir -p "${OUTPUT_DIR}"
  ensure_env_file
  export_env_file_safely "${ENV_FILE}"
  go_bin="$(resolve_selected_go_bin)" || fail "unable to resolve selected Go toolchain binary"

  ldflags="$(build_ldflags)"
  output_bin="${OUTPUT_DIR}/xg2g"

  cmd=(env GOTOOLCHAIN=local "${go_bin}" build -trimpath -buildvcs=false -ldflags "${ldflags}")
  if [[ "${UI_MODE}" -eq 1 ]]; then
    export XG2G_LISTEN="${XG2G_LISTEN:-:8080}"
    export XG2G_UI_DEV_PROXY_URL="${XG2G_UI_DEV_PROXY_URL:-http://127.0.0.1:5173}"
    output_bin="${OUTPUT_DIR}/xg2g-dev"
    cmd+=(-tags=dev)
  fi
  cmd+=(-o "${output_bin}" ./cmd/daemon)

  cd "${BACKEND_ROOT}"
  "${cmd[@]}"
  exec "${output_bin}"
}

main "$@"
