#!/usr/bin/env bash
set -euo pipefail

CTID="${XG2G_DEPLOY_CTID:-110}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BINARY="${ROOT}/bin/xg2g"
STAGING_BINARY="/srv/xg2g-staging/xg2g-staging-binary"
PRODUCTION_BINARY="/srv/xg2g/xg2g-production-binary"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

wait_healthy() {
  local container="$1"
  local i status
  for ((i = 0; i < 90; i++)); do
    status="$(pct exec "${CTID}" -- docker inspect --format '{{.State.Health.Status}}' "${container}" 2>/dev/null || true)"
    [[ "${status}" == "healthy" ]] && return 0
    [[ "${status}" == "unhealthy" ]] && die "${container} became unhealthy"
    sleep 1
  done
  die "timed out waiting for ${container} health"
}

promote_production="${XG2G_PROMOTE_PRODUCTION:-false}"
case "${promote_production,,}" in
  true|1|yes|on)
    promote_production=true
    ;;
  false|0|no|off|"")
    promote_production=false
    ;;
  *)
    die "invalid XG2G_PROMOTE_PRODUCTION=${promote_production}"
    ;;
esac

deploy_binary() {
  local destination="$1"
  local next="${destination}.next"
  pct push "${CTID}" "${BINARY}" "${next}"
  pct exec "${CTID}" -- chmod 0755 "${next}"
  pct exec "${CTID}" -- mv "${next}" "${destination}"
}

[[ -x "${BINARY}" ]] || die "missing build artifact ${BINARY}; run make build-with-ui first"

cd "${ROOT}"
branch="$(git branch --show-current)"
[[ -n "${branch}" ]] || die "detached HEAD is not deployable"
git fetch origin "${branch}" --quiet
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || die "tracked working tree changes must be committed"
[[ -z "$(git rev-list "origin/${branch}..HEAD")" ]] || die "HEAD must be pushed to origin/${branch} before deployment"

echo "Deploying $(git rev-parse --short=12 HEAD) to staging..."
deploy_binary "${STAGING_BINARY}"
pct exec "${CTID}" -- docker compose --project-directory /srv/xg2g-staging -f /srv/xg2g-staging/docker-compose.yml up -d --force-recreate
wait_healthy xg2g-staging
pct exec "${CTID}" -- docker exec xg2g-staging xg2g healthcheck -mode live -port 8089

expected="$(sha256sum "${BINARY}" | awk '{print $1}')"
staging="$(pct exec "${CTID}" -- docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
[[ "${expected}" == "${staging}" ]] || die "staging binary hash mismatch"

if [[ "${promote_production}" != "true" ]]; then
  echo "Staging deployment complete on port 8089: ${expected}"
  echo "Production was not changed. Set XG2G_PROMOTE_PRODUCTION=true to promote explicitly."
  exit 0
fi

echo "Staging healthy; promoting identical binary to production..."
deploy_binary "${PRODUCTION_BINARY}"
pct exec "${CTID}" -- systemctl restart xg2g
wait_healthy xg2g
pct exec "${CTID}" -- docker exec xg2g xg2g healthcheck -mode live

production="$(pct exec "${CTID}" -- docker exec xg2g sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
[[ "${expected}" == "${staging}" && "${expected}" == "${production}" ]] || die "deployed binary hash mismatch"
echo "Deployment complete: ${expected}"
