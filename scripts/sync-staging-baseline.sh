#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_RUNTIME_HOST:-xg2g-dev}"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

[[ "${1:-}" == "--confirm-staging-baseline" ]] ||
  die "explicit confirmation required: $0 --confirm-staging-baseline"
[[ "$#" -eq 1 ]] || die "unknown arguments: ${*:2}"

ssh -o BatchMode=yes -o ConnectTimeout=8 "${REMOTE_HOST}" bash -s <<'REMOTE'
set -euo pipefail

destination="/srv/xg2g-staging/xg2g-staging-binary"
manifest="/srv/xg2g-staging/deploy-manifest"
next_manifest="${manifest}.next"
compose_file="/srv/xg2g-staging/docker-compose.yml"
next_compose="${compose_file}.next"
storage_overlay="/srv/xg2g-staging/docker-compose.storage.yml"
candidate_overlay="/srv/xg2g-staging/docker-compose.candidate.yml"

wait_healthy() {
  local container="$1"
  local port="$2"
  local i status
  for ((i = 0; i < 90; i++)); do
    status="$(docker inspect --format '{{.State.Health.Status}}' "${container}" 2>/dev/null || true)"
    if [[ "${status}" == "healthy" ]] &&
      curl -fsS --max-time 5 "http://127.0.0.1:${port}/healthz" >/dev/null; then
      return 0
    fi
    [[ "${status}" != "unhealthy" ]] || return 1
    sleep 1
  done
  return 1
}

wait_healthy xg2g 8088
wait_healthy xg2g-staging 8089

[[ -f "${compose_file}" ]] || {
  echo "ERROR: staging compose file is missing: ${compose_file}" >&2
  exit 1
}

production_version="$(docker exec xg2g /usr/local/bin/xg2g --version)"
production_sha="$(docker exec xg2g sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
production_image_id="$(docker inspect --format '{{.Image}}' xg2g)"
production_image_ref="$(
  docker image inspect "${production_image_id}" --format '{{index .RepoDigests 0}}'
)"
production_commit="$(sed -n 's/.*(commit: \([0-9a-fA-F]\{7,40\}\), built:.*/\1/p' <<<"${production_version}")"
production_release="$(awk '{print $1}' <<<"${production_version}")"
[[ "${production_sha}" =~ ^[0-9a-f]{64}$ ]]
[[ "${production_image_id}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "${production_image_ref}" =~ ^[a-z0-9./_-]+@sha256:[0-9a-f]{64}$ ]]
[[ "${production_commit}" =~ ^[0-9a-fA-F]{7,40}$ ]]
[[ "${production_release}" =~ ^[vV0-9][A-Za-z0-9.+_-]*$ ]]

rm -f "${next_compose}" "${next_manifest}"
awk -v image="${production_image_ref}" -v binary="${destination}" '
  /^[[:space:]]+image:[[:space:]]*/ {
    print "    image: " image
    image_count++
    next
  }
  index($0, binary) > 0 && index($0, "/usr/local/bin/xg2g") > 0 {
    mount_count++
    next
  }
  /^[[:space:]]*-[[:space:]]*/ && index($0, "8089:8089") > 0 {
    print "      - \"127.0.0.1:8089:8089\""
    port_count++
    next
  }
  { print }
  END {
    if (image_count != 1) {
      print "ERROR: expected exactly one staging image declaration" > "/dev/stderr"
      exit 1
    }
    if (mount_count > 1) {
      print "ERROR: found multiple governed staging binary mounts" > "/dev/stderr"
      exit 1
    }
    if (port_count != 1) {
      print "ERROR: expected exactly one staging port declaration" > "/dev/stderr"
      exit 1
    }
  }
' "${compose_file}" >"${next_compose}"
mv "${next_compose}" "${compose_file}"
rm -f "${candidate_overlay}"

compose_args=(--project-directory /srv/xg2g-staging -f "${compose_file}")
[[ ! -f "${storage_overlay}" ]] || compose_args+=(-f "${storage_overlay}")
docker compose "${compose_args[@]}" up -d --force-recreate
wait_healthy xg2g-staging 8089 || {
  docker logs --tail 100 xg2g-staging >&2 || true
  echo "ERROR: staging did not become healthy with the production baseline" >&2
  exit 1
}

staging_version="$(docker exec xg2g-staging /usr/local/bin/xg2g --version)"
staging_sha="$(docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
staging_image_id="$(docker inspect --format '{{.Image}}' xg2g-staging)"
staging_mount="$(
  docker inspect --format '{{range .Mounts}}{{if eq .Destination "/usr/local/bin/xg2g"}}{{println .Source}}{{end}}{{end}}' xg2g-staging |
    awk 'NF'
)"
staging_published="$(docker port xg2g-staging 8089/tcp | paste -sd, -)"
[[ "${staging_version}" == "${production_version}" ]] || {
  echo "ERROR: staging version metadata does not match production" >&2
  exit 1
}
[[ "${staging_sha}" == "${production_sha}" ]] || {
  echo "ERROR: staging artifact does not match production" >&2
  exit 1
}
[[ "${staging_image_id}" == "${production_image_id}" ]] || {
  echo "ERROR: staging image does not match production" >&2
  exit 1
}
[[ -z "${staging_mount}" ]] || {
  echo "ERROR: staging still has a binary override in baseline mode" >&2
  exit 1
}
[[ "${staging_published}" == 127.0.0.1:* || "${staging_published}" == "[::1]:"* ]] || {
  echo "ERROR: staging port 8089 is not loopback-only" >&2
  exit 1
}

{
  printf 'schema=2\n'
  printf 'mode=baseline\n'
  printf 'source=production\n'
  printf 'version=%s\n' "${production_release}"
  printf 'commit=%s\n' "${production_commit}"
  printf 'sha256=%s\n' "${production_sha}"
  printf 'image_id=%s\n' "${production_image_id}"
  printf 'image_ref=%s\n' "${production_image_ref}"
  printf 'deployed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"${next_manifest}"
mv "${next_manifest}" "${manifest}"

printf 'Staging baseline synchronized: version=%s commit=%s sha256=%s image=%s port=8089\n' \
  "${production_release}" "${production_commit}" "${production_sha}" "${production_image_id}"
REMOTE

"${ROOT}/scripts/check-deployment-state.sh"
