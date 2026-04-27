#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_VERSION_FILE="${REPO_ROOT}/.node-version"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

command -v node >/dev/null 2>&1 || fail "missing required command: node"

[[ -f "${NODE_VERSION_FILE}" ]] || fail "missing ${NODE_VERSION_FILE}"

expected="$(tr -d '[:space:]' < "${NODE_VERSION_FILE}")"
[[ "${expected}" =~ ^([0-9]+) ]] || fail "invalid Node version pin in ${NODE_VERSION_FILE}: ${expected}"
expected_major="${BASH_REMATCH[1]}"

actual="$(node -p 'process.versions.node')"
actual_major="${actual%%.*}"
node_path="$(command -v node)"

if [[ "${actual_major}" != "${expected_major}" ]]; then
  fail "node version mismatch: expected Node ${expected} from ${NODE_VERSION_FILE}, got ${actual} at ${node_path}. Run: nvm install && nvm use, or mise install."
fi
