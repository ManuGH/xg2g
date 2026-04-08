#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
SCRIPT="${ROOT}/backend/scripts/ci/resolve-lint-scope.sh"

assert_contains() {
  local haystack="$1"
  local needle="$2"
  if [[ "${haystack}" != *"${needle}"* ]]; then
    echo "expected output to contain: ${needle}" >&2
    echo "--- output ---" >&2
    printf '%s\n' "${haystack}" >&2
    exit 1
  fi
}

tmpdir="$(mktemp -d)"
trap 'rm -rf "${tmpdir}"' EXIT

changed_files="${tmpdir}/changed-files.txt"

cat > "${changed_files}" <<'EOF'
frontend/webui/src/App.tsx
EOF
output="$(XG2G_CI_CHANGED_FILES_PATH="${changed_files}" "${SCRIPT}" pull_request base head)"
assert_contains "${output}" "env_access_needed=false"
assert_contains "${output}" "deprecations_needed=false"
assert_contains "${output}" "webui_lint_needed=true"
assert_contains "${output}" "forced_full_scope=false"

cat > "${changed_files}" <<'EOF'
backend/internal/app/bootstrap/wiring_helpers.go
docs/deprecations.json
EOF
output="$(XG2G_CI_CHANGED_FILES_PATH="${changed_files}" "${SCRIPT}" pull_request base head)"
assert_contains "${output}" "env_access_needed=true"
assert_contains "${output}" "deprecations_needed=true"
assert_contains "${output}" "webui_lint_needed=false"

output="$("${SCRIPT}" pull_request missing-base missing-head || true)"
assert_contains "${output}" "env_access_needed=true"
assert_contains "${output}" "deprecations_needed=true"
assert_contains "${output}" "webui_lint_needed=true"
assert_contains "${output}" "forced_full_scope=true"

output="$("${SCRIPT}" workflow_dispatch)"
assert_contains "${output}" "env_access_needed=true"
assert_contains "${output}" "deprecations_needed=true"
assert_contains "${output}" "webui_lint_needed=true"
assert_contains "${output}" "forced_full_scope=true"
assert_contains "${output}" "reason=workflow_dispatch_full_run"

echo "resolve-lint-scope tests passed"
