#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ENV_FILE="${REPO_ROOT}/.env"
WEBUI_NODE_MODULES="${REPO_ROOT}/frontend/webui/node_modules"
VARIABLES_FILE="${REPO_ROOT}/mk/variables.mk"
GO_TOOLCHAIN_CHECK="${REPO_ROOT}/backend/scripts/check-go-toolchain.sh"
LIB_ENV="${REPO_ROOT}/backend/scripts/lib/env.sh"
NOTES=()

source "${LIB_ENV}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

note() {
  NOTES+=("$1")
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

read_make_var() {
  local name="$1"
  local value

  value="$(sed -nE "s/^${name}[[:space:]]*:?=[[:space:]]*(.+)$/\\1/p" "${VARIABLES_FILE}" | head -1)"
  [[ -n "${value}" ]] || fail "could not read ${name} from ${VARIABLES_FILE}"
  printf '%s' "$(trim_ascii_whitespace "${value}")"
}

tool_path() {
  local tool="$1"
  local gobin

  gobin="$(go env GOBIN)"
  if [[ -n "${gobin}" && -x "${gobin}/${tool}" ]]; then
    printf '%s\n' "${gobin}/${tool}"
    return 0
  fi

  printf '%s\n' "$(go env GOPATH)/bin/${tool}"
}

check_golangci_lint() {
  local expected tool actual

  expected="$(read_make_var GOLANGCI_LINT_VERSION)"
  tool="$(tool_path golangci-lint)"
  [[ -x "${tool}" ]] || fail "golangci-lint not found at ${tool} (run: make dev-tools)"

  actual="$("${tool}" version 2>/dev/null | sed -nE 's/.*version ([0-9]+\.[0-9]+\.[0-9]+).*/v\1/p' | head -1)"
  [[ "${actual}" == "${expected}" ]] || fail "golangci-lint version mismatch: expected ${expected}, got ${actual:-unknown} (run: make dev-tools)"
}

check_govulncheck() {
  local tool

  tool="$(tool_path govulncheck)"
  [[ -x "${tool}" ]] || fail "govulncheck not found at ${tool} (run: make dev-tools)"
}

check_env_file() {
  local e2_host

  [[ -f "${ENV_FILE}" ]] || fail "missing ${ENV_FILE} (run: make install)"
  read_env_value "${ENV_FILE}" XG2G_DECISION_SECRET >/dev/null 2>&1 || fail "missing XG2G_DECISION_SECRET in ${ENV_FILE} (run: make install)"

  e2_host="$(read_env_value "${ENV_FILE}" XG2G_E2_HOST 2>/dev/null || true)"
  if [[ -z "${e2_host}" ]]; then
    note "${ENV_FILE} does not define XG2G_E2_HOST yet; real playback tests still need a reachable receiver."
  elif [[ "${e2_host}" == "http://192.168.1.100" ]]; then
    note "${ENV_FILE} still contains the example XG2G_E2_HOST; real playback tests still need a reachable receiver."
  fi
}

check_webui_deps() {
  [[ -d "${WEBUI_NODE_MODULES}" ]] || fail "missing ${WEBUI_NODE_MODULES} (run: make install)"
}

check_docker_runtime_note() {
  if ! docker info >/dev/null 2>&1; then
    note "Docker CLI is installed, but the engine is not reachable. 'make start', 'make start-gpu', and 'make start-nvidia' need a running Docker daemon."
  fi
}

main() {
  need_cmd go
  need_cmd node
  need_cmd npm
  need_cmd docker
  need_cmd openssl
  need_cmd python3
  need_cmd make

  "${GO_TOOLCHAIN_CHECK}"
  check_golangci_lint
  check_govulncheck
  check_env_file
  check_webui_deps
  check_docker_runtime_note

  echo "✅ Developer workspace checks passed."
  echo "INFO: This verifies the local workspace only. Container runtime and receiver reachability are validated by the corresponding start path."
  if [[ "${#NOTES[@]}" -gt 0 ]]; then
    local msg
    for msg in "${NOTES[@]}"; do
      echo "NOTE: ${msg}"
    done
  fi
}

main "$@"
