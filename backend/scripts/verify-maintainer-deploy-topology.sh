#!/usr/bin/env bash
# shellcheck disable=SC2016 # Contract assertions intentionally contain literal shell expressions.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAGING="${REPO_ROOT}/scripts/deploy-staging-fast.sh"
RECONCILE="${REPO_ROOT}/scripts/reconcile_xg2g.sh"
PROMOTE="${REPO_ROOT}/scripts/promote_production.sh"
RELEASE_STAGE="${REPO_ROOT}/scripts/stage-release-candidate.sh"
STATE_CHECK="${REPO_ROOT}/scripts/check-deployment-state.sh"
BASELINE_SYNC="${REPO_ROOT}/scripts/sync-staging-baseline.sh"
STATE_LIB="${REPO_ROOT}/scripts/lib/deployment-state.sh"
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

bash -n "${STAGING}" "${RECONCILE}" "${PROMOTE}" "${RELEASE_STAGE}" "${STATE_CHECK}" "${BASELINE_SYNC}" "${STATE_LIB}"

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
assert_not_contains "${PROMOTE}" "rollback_binary"
assert_contains "${PROMOTE}" 'xg2g-admin update --ref'
assert_contains "${PROMOTE}" 'deployment.state=candidate'
assert_contains "${RELEASE_STAGE}" 'mode=candidate'
assert_contains "${RELEASE_STAGE}" 'docker pull "${image_tag}"'
assert_contains "${STAGING}" 'mode=candidate'
assert_contains "${BASELINE_SYNC}" 'mode=baseline'
assert_contains "${STATE_CHECK}" 'deployment.state='
assert_contains "${STATE_CHECK}" 'REMOTE_HOST="${XG2G_RUNTIME_HOST:-xg2g-dev}"'
assert_contains "${BASELINE_SYNC}" 'production_image_ref='
assert_contains "${BASELINE_SYNC}" 'rm -f "${candidate_overlay}"'
assert_contains "${BASELINE_SYNC}" '127.0.0.1:8089:8089'
assert_contains "${BASELINE_SYNC}" '--confirm-staging-baseline'

assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g-build`'
assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g-staging`'
assert_contains "${WORKFLOW_DOC}" 'LXC 110 `/srv/xg2g`'
assert_contains "${AGENT_RULES}" 'LXC 110 `/srv/xg2g-build` is the only Linux fast-iteration build checkout.'
assert_contains "${AGENT_RULES}" 'LXC 110 `/srv/xg2g-staging` is a deployment surface, not a Git checkout.'
assert_contains "${AGENT_RULES}" 'Before selecting a SemVer or editing release metadata, complete a'
assert_contains "${AGENT_RULES}" 'Never use public tags or patch versions as release-pipeline experiments.'
assert_contains "${AGENT_RULES}" 'Treat a successful stable release as a terminal state.'
assert_contains "${AGENT_RULES}" 'Before changing either live environment, capture the complete'
assert_contains "${AGENT_RULES}" 'Runtime lifecycle has exactly two valid steady states:'
assert_contains "${AGENT_RULES}" 'If an explicitly authorized out-of-band production'
assert_not_contains "${AGENT_RULES}" 'instead of binary promotion'

tmp_repo="$(mktemp -d)"
trap 'rm -rf "${tmp_repo}"' EXIT
git -C "${tmp_repo}" init --quiet
git -C "${tmp_repo}" -c user.name=test -c user.email=test@example.invalid commit --allow-empty -m base --quiet
base_commit="$(git -C "${tmp_repo}" rev-parse HEAD)"
git -C "${tmp_repo}" -c user.name=test -c user.email=test@example.invalid commit --allow-empty -m candidate --quiet
candidate_commit="$(git -C "${tmp_repo}" rev-parse HEAD)"

# shellcheck source=scripts/lib/deployment-state.sh
source "${STATE_LIB}"
test "$(classify_deployment_state "${tmp_repo}" "${base_commit}" aaa "${base_commit}" aaa baseline "${base_commit}" aaa image-a image-a "")" = baseline
test "$(classify_deployment_state "${tmp_repo}" "${base_commit}" aaa "${candidate_commit}" bbb candidate "${candidate_commit}" bbb image-a image-a /candidate)" = candidate
test "$(classify_deployment_state "${tmp_repo}" "${candidate_commit}" bbb "${base_commit}" aaa candidate "${base_commit}" aaa image-a image-a /candidate || true)" = stale
test "$(classify_deployment_state "${tmp_repo}" "${base_commit}" aaa "${candidate_commit}" bbb baseline "${base_commit}" aaa image-a image-a "" || true)" = untracked_candidate
test "$(classify_deployment_state "${tmp_repo}" "${base_commit}" aaa "${base_commit}" aaa baseline "${base_commit}" aaa image-a image-b "" || true)" = runtime_drift

printf 'OK: maintainer deployment topology is fail-closed and matches LXC 110.\n'
