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
LANDING_LIB="${REPO_ROOT}/scripts/lib/staging-landing.sh"
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

bash -n "${STAGING}" "${RECONCILE}" "${PROMOTE}" "${RELEASE_STAGE}" "${STATE_CHECK}" "${BASELINE_SYNC}" "${STATE_LIB}" "${LANDING_LIB}"

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

# Staging landing guard: the fast deploy must consult it before anything is built.
assert_contains "${STAGING}" 'source "${ROOT}/scripts/lib/staging-landing.sh"'
assert_contains "${STAGING}" 'classify_staging_candidate "${ROOT}" "${commit}" origin/main'
assert_contains "${STAGING}" 'classify_staging_replacement "${ROOT}" "${running_commit}" "${commit}" origin/main'
guard_line="$(grep -n 'classify_staging_replacement "${ROOT}"' "${STAGING}" | head -n 1 | cut -d: -f1)"
build_line="$(grep -n 'Preparing commit' "${STAGING}" | head -n 1 | cut -d: -f1)"
[[ -n "${guard_line}" && -n "${build_line}" && "${guard_line}" -lt "${build_line}" ]] ||
  fail "the staging landing guard must run before the build in ${STAGING#"${REPO_ROOT}"/}"

# shellcheck source=scripts/lib/staging-landing.sh
source "${LANDING_LIB}"
# GitHub is replaced by these stubs: open pull-request heads, the states of the
# pull requests containing a commit, and whether GitHub answers at all.
landing_heads=""
landing_states=""
landing_lookup_ok=1
staging_open_pr_heads() {
  [[ "${landing_lookup_ok}" == "1" ]] || return 1
  [[ -z "${landing_heads}" ]] || printf '%s\n' "${landing_heads}"
}
staging_pr_states_for_commit() {
  [[ "${landing_lookup_ok}" == "1" ]] || return 1
  [[ -z "${landing_states}" ]] || printf '%s\n' "${landing_states}"
}
expect_landing() {
  local want_state="$1"
  local want_rc="$2"
  shift 2
  local got rc=0
  got="$("$@")" || rc=$?
  [[ "${got}" == "${want_state}" && "${rc}" == "${want_rc}" ]] ||
    fail "staging landing guard: $* gave ${got:-nothing}/${rc}, expected ${want_state}/${want_rc}"
}
git_test() {
  git -C "${tmp_repo}" -c user.name=test -c user.email=test@example.invalid "$@"
}

# side1 <- side2 and other1 branch off candidate_commit; main moves on to main2,
# which neither side contains.
git_test branch landing-main "${candidate_commit}"
git_test checkout --quiet landing-main
git_test commit --allow-empty -m main2 --quiet
main2="$(git -C "${tmp_repo}" rev-parse HEAD)"
git_test checkout --quiet -b landing-side "${candidate_commit}"
git_test commit --allow-empty -m side1 --quiet
side1="$(git -C "${tmp_repo}" rev-parse HEAD)"
git_test commit --allow-empty -m side2 --quiet
side2="$(git -C "${tmp_repo}" rev-parse HEAD)"
git_test checkout --quiet -b landing-other "${candidate_commit}"
git_test commit --allow-empty -m other1 --quiet
other1="$(git -C "${tmp_repo}" rev-parse HEAD)"
unknown_commit="0123456789abcdef0123456789abcdef01234567"

expect_landing descendant 0 classify_staging_replacement "${tmp_repo}" "${base_commit}" "${other1}" landing-main
expect_landing none 0 classify_staging_replacement "${tmp_repo}" "" "${other1}" landing-main
expect_landing same 0 classify_staging_replacement "${tmp_repo}" "${side1}" "${side1}" landing-main
expect_landing same 0 classify_staging_replacement "${tmp_repo}" "${side1:0:8}" "${side1}" landing-main
expect_landing descendant 0 classify_staging_replacement "${tmp_repo}" "${side1}" "${side2}" landing-main
expect_landing in_main 0 classify_staging_replacement "${tmp_repo}" "${main2}" "${other1}" landing-main
landing_heads="${side1}"
expect_landing in_pr 0 classify_staging_replacement "${tmp_repo}" "${side1}" "${other1}" landing-main
landing_heads=""
landing_states="MERGED"
expect_landing in_pr 0 classify_staging_replacement "${tmp_repo}" "${side1}" "${other1}" landing-main
landing_states=$'CLOSED\nOPEN'
expect_landing in_pr 0 classify_staging_replacement "${tmp_repo}" "${unknown_commit}" "${other1}" landing-main
# The losses the guard exists for: no pull request, or only a closed one.
landing_states=""
expect_landing unlanded 1 classify_staging_replacement "${tmp_repo}" "${side1}" "${other1}" landing-main
landing_states="CLOSED"
expect_landing unlanded 1 classify_staging_replacement "${tmp_repo}" "${side1}" "${other1}" landing-main
expect_landing unlanded 1 classify_staging_replacement "${tmp_repo}" "${side2}" "${side1}" landing-main
expect_landing unlanded 1 classify_staging_replacement "${tmp_repo}" "${unknown_commit}" "${other1}" landing-main
expect_landing unlanded 1 classify_staging_replacement "${tmp_repo}" "not-a-commit" "${other1}" landing-main
landing_lookup_ok=0
expect_landing lookup_failed 1 classify_staging_replacement "${tmp_repo}" "${side1}" "${other1}" landing-main
expect_landing in_main 0 classify_staging_replacement "${tmp_repo}" "${main2}" "${other1}" landing-main
landing_lookup_ok=1
landing_states=""

expect_landing in_main 0 classify_staging_candidate "${tmp_repo}" "${main2}" landing-main
landing_heads="${other1}"
expect_landing in_open_pr 0 classify_staging_candidate "${tmp_repo}" "${other1}" landing-main
expect_landing no_pr 1 classify_staging_candidate "${tmp_repo}" "${side1}" landing-main
landing_heads=""
expect_landing no_pr 1 classify_staging_candidate "${tmp_repo}" "${other1}" landing-main
landing_lookup_ok=0
expect_landing lookup_failed 1 classify_staging_candidate "${tmp_repo}" "${other1}" landing-main
landing_lookup_ok=1

printf 'OK: maintainer deployment topology is fail-closed and matches LXC 110.\n'
