#!/usr/bin/env bash
# shellcheck disable=SC2016 # Contract assertions intentionally contain literal shell expressions.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAGING="${REPO_ROOT}/scripts/deploy-staging-fast.sh"
RECONCILE="${REPO_ROOT}/scripts/reconcile_xg2g.sh"
PROMOTE="${REPO_ROOT}/scripts/promote_production.sh"
WORKFLOW_DOC="${REPO_ROOT}/docs/ops/XG2G_SYNC_WORKFLOW.md"
AGENT_RULES="${REPO_ROOT}/AGENTS.md"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file="$1"
  local needle="$2"
  grep -Fq -- "${needle}" "${file}" ||
    fail "expected '${needle}' in ${file#"${REPO_ROOT}"/}"
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  if grep -Fq -- "${needle}" "${file}"; then
    fail "unexpected '${needle}' in ${file#"${REPO_ROOT}"/}"
  fi
}

bash -n "${STAGING}" "${RECONCILE}" "${PROMOTE}"

assert_contains "${STAGING}" 'REMOTE_HOST="${XG2G_DEPLOY_HOST:-xg2g-dev}"'
assert_contains "${STAGING}" 'REMOTE_BUILD_ROOT="${XG2G_DEPLOY_BUILD_ROOT:-/srv/xg2g-build}"'
assert_not_contains "${STAGING}" "REMOTE_SOURCE_ROOT"
assert_not_contains "${STAGING}" "rm -rf"

assert_contains "${RECONCILE}" 'REMOTE_HOST="${XG2G_RECONCILE_HOST:-xg2g-dev}"'
assert_contains "${RECONCILE}" 'REMOTE_BUILD_ROOT="${XG2G_RECONCILE_BUILD_ROOT:-/srv/xg2g-build}"'
assert_not_contains "${RECONCILE}" "REMOTE_SOURCE_ROOT"
assert_not_contains "${RECONCILE}" "/root/xg2g"
assert_not_contains "${RECONCILE}" "pct exec"
assert_not_contains "${RECONCILE}" "rm -rf"

assert_contains "${PROMOTE}" 'REMOTE_HOST="${XG2G_DEPLOY_HOST:-pve2}"'
assert_not_contains "${PROMOTE}" "root@10."

assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g-build`'
assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g-staging`'
assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g`'
assert_contains "${AGENT_RULES}" 'LXC 110 `/srv/xg2g-build` is the only Linux fast-iteration build checkout.'
assert_contains "${AGENT_RULES}" 'LXC 110 `/srv/xg2g-staging` is a deployment surface, not a Git checkout.'

printf 'OK: maintainer deployment topology is fail-closed and matches LXC 110.\n'
