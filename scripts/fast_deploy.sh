#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-root@10.10.55.2}"
REMOTE_SOURCE_ROOT="${XG2G_DEPLOY_SOURCE_ROOT:-/root/xg2g}"
REMOTE_BUILD_ROOT="${XG2G_DEPLOY_BUILD_ROOT:-/root/xg2g-build}"
CTID="${XG2G_DEPLOY_CTID:-110}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

if [[ "${1:-}" != "--confirm-staging" ]]; then
  die "staging deployment requires explicit confirmation: ./scripts/fast_deploy.sh --confirm-staging"
fi
shift
[[ "$#" -eq 0 ]] || die "unknown arguments: $*"

if [[ "${XG2G_PROMOTE_PRODUCTION:-0}" =~ ^(1|true|yes|on)$ ]]; then
  die "fast_deploy.sh is staging-only; use scripts/promote_production.sh --confirm-production"
fi

cd "${ROOT}"
branch="$(git branch --show-current)"
[[ -n "${branch}" ]] || die "detached HEAD is not deployable"
[[ "${branch}" =~ ^[A-Za-z0-9._/-]+$ ]] || die "unsafe branch name: ${branch}"
[[ -z "$(git status --porcelain)" ]] || die "working tree must be completely clean before deployment"

git fetch origin "${branch}" --quiet
commit="$(git rev-parse HEAD)"
origin_commit="$(git rev-parse "origin/${branch}")"
[[ "${commit}" == "${origin_commit}" ]] || die "HEAD must exactly match pushed origin/${branch} before deployment"

echo "Preparing commit ${commit} in ${REMOTE_HOST}:${REMOTE_BUILD_ROOT}..."
ssh "${REMOTE_HOST}" bash -s -- "${REMOTE_SOURCE_ROOT}" "${REMOTE_BUILD_ROOT}" "${branch}" "${commit}" <<'REMOTE'
set -euo pipefail
source_root="$1"
build_root="$2"
branch="$3"
commit="$4"

origin_url="$(git -C "${source_root}" remote get-url origin)"
if [[ ! -d "${build_root}/.git" ]]; then
  [[ ! -e "${build_root}" ]] || {
    echo "ERROR: ${build_root} exists but is not a Git checkout" >&2
    exit 1
  }
  git clone "${origin_url}" "${build_root}"
fi

cd "${build_root}"
[[ -z "$(git status --porcelain)" ]] || {
  echo "ERROR: remote build checkout is dirty" >&2
  exit 1
}
git remote set-url origin "${origin_url}"
git fetch origin "${branch}" --quiet
[[ "$(git rev-parse "origin/${branch}")" == "${commit}" ]] || {
  echo "ERROR: remote origin/${branch} does not match requested commit ${commit}" >&2
  exit 1
}
git switch --detach "${commit}"
[[ "$(git rev-parse HEAD)" == "${commit}" ]] || exit 1
make build-with-ui
REMOTE

remote_binary="${REMOTE_BUILD_ROOT}/bin/xg2g"
expected_sha="$(ssh "${REMOTE_HOST}" "sha256sum '${remote_binary}' | awk '{print \$1}'")"
[[ -n "${expected_sha}" ]] || die "could not hash remote build artifact"

echo "Deploying ${commit} (${expected_sha}) to staging :8089..."
ssh "${REMOTE_HOST}" bash -s -- "${CTID}" "${remote_binary}" "${expected_sha}" "${commit}" <<'REMOTE'
set -euo pipefail
ctid="$1"
binary="$2"
expected_sha="$3"
commit="$4"
next="/srv/xg2g-staging/xg2g-staging-binary.next"
destination="/srv/xg2g-staging/xg2g-staging-binary"

pct push "${ctid}" "${binary}" "${next}"
pct exec "${ctid}" -- chmod 0755 "${next}"
pct exec "${ctid}" -- mv "${next}" "${destination}"
pct exec "${ctid}" -- docker compose --project-directory /srv/xg2g-staging -f /srv/xg2g-staging/docker-compose.yml up -d --force-recreate

healthy=0
for ((i = 0; i < 90; i++)); do
  status="$(pct exec "${ctid}" -- docker inspect --format '{{.State.Health.Status}}' xg2g-staging 2>/dev/null || true)"
  if [[ "${status}" == "healthy" ]] && pct exec "${ctid}" -- curl -fsS http://127.0.0.1:8089/healthz >/dev/null; then
    healthy=1
    break
  fi
  [[ "${status}" != "unhealthy" ]] || break
  sleep 1
done
[[ "${healthy}" == "1" ]] || {
  pct exec "${ctid}" -- docker logs --tail 100 xg2g-staging >&2 || true
  echo "ERROR: staging did not become healthy" >&2
  exit 1
}

running_sha="$(pct exec "${ctid}" -- docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
[[ "${running_sha}" == "${expected_sha}" ]] || {
  echo "ERROR: running staging hash ${running_sha} != ${expected_sha}" >&2
  exit 1
}
pct exec "${ctid}" -- sh -c "printf '%s %s\n' '${commit}' '${expected_sha}' > /srv/xg2g-staging/deploy-manifest"
REMOTE

echo "Staging deployment complete: commit=${commit} sha256=${expected_sha} port=8089"
echo "Production :8088 was not touched."
