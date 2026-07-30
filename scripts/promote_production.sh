#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-pve2}"
CTID="${XG2G_DEPLOY_CTID:-110}"
RELEASE_REF=""
CONFIRMED=0

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --ref)
      [[ "$#" -ge 2 ]] || die "--ref requires a release tag"
      RELEASE_REF="$2"
      shift 2
      ;;
    --confirm-production)
      CONFIRMED=1
      shift
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ "${CONFIRMED}" == "1" ]] || die "explicit --confirm-production is required"
[[ "${CTID}" =~ ^[0-9]+$ ]] || die "XG2G_DEPLOY_CTID must be numeric"
[[ "${RELEASE_REF}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  die "--ref must be an explicit SemVer release tag"

git -C "${ROOT}" fetch origin --tags --quiet
release_commit="$(git -C "${ROOT}" rev-parse --verify "${RELEASE_REF}^{commit}")" ||
  die "release tag is not available: ${RELEASE_REF}"

state="$("${ROOT}/scripts/check-deployment-state.sh")" ||
  die "staging is not a valid release candidate"
grep -Fqx 'deployment.state=candidate' <<<"${state}" ||
  die "production promotion requires deployment.state=candidate"
grep -Fqx "staging.commit=${release_commit}" <<<"${state}" ||
  die "staging commit does not match ${RELEASE_REF}"
grep -Fqx "staging.version=${RELEASE_REF#v}" <<<"${state}" ||
  grep -Fqx "staging.version=${RELEASE_REF}" <<<"${state}" ||
  die "staging version does not match ${RELEASE_REF}"

ssh -o BatchMode=yes -o ConnectTimeout=8 "${REMOTE_HOST}" \
  pct exec "${CTID}" -- /usr/local/sbin/xg2g-admin update --ref "${RELEASE_REF}"

"${ROOT}/scripts/sync-staging-baseline.sh" --confirm-staging-baseline

printf 'Production promotion complete: ref=%s commit=%s state=baseline\n' \
  "${RELEASE_REF}" "${release_commit}"
