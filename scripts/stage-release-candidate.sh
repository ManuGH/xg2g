#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_RUNTIME_HOST:-xg2g-dev}"
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
    --confirm-staging)
      CONFIRMED=1
      shift
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[[ "${CONFIRMED}" == "1" ]] || die "explicit --confirm-staging is required"
[[ "${RELEASE_REF}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] ||
  die "--ref must be an explicit SemVer release tag"

git -C "${ROOT}" fetch origin --tags --quiet
expected_commit="$(git -C "${ROOT}" rev-parse --verify "${RELEASE_REF}^{commit}")" ||
  die "release tag is not available: ${RELEASE_REF}"

ssh -o BatchMode=yes -o ConnectTimeout=8 "${REMOTE_HOST}" bash -s -- \
  "${RELEASE_REF}" "${expected_commit}" <<'REMOTE'
set -euo pipefail

release_ref="$1"
expected_commit="$2"
image_tag="ghcr.io/manugh/xg2g:${release_ref}"
compose_file="/srv/xg2g-staging/docker-compose.yml"
next_compose="${compose_file}.next"
storage_overlay="/srv/xg2g-staging/docker-compose.storage.yml"
candidate_overlay="/srv/xg2g-staging/docker-compose.candidate.yml"
manifest="/srv/xg2g-staging/deploy-manifest"
next_manifest="${manifest}.next"
binary="/srv/xg2g-staging/xg2g-staging-binary"

wait_healthy() {
  local i status
  for ((i = 0; i < 90; i++)); do
    status="$(docker inspect --format '{{.State.Health.Status}}' xg2g-staging 2>/dev/null || true)"
    if [[ "${status}" == "healthy" ]] &&
      curl -fsS --max-time 5 http://127.0.0.1:8089/healthz >/dev/null; then
      return 0
    fi
    [[ "${status}" != "unhealthy" ]] || return 1
    sleep 1
  done
  return 1
}

[[ -f "${compose_file}" ]] || {
  echo "ERROR: staging compose file is missing: ${compose_file}" >&2
  exit 1
}

docker pull "${image_tag}"
image_id="$(docker image inspect "${image_tag}" --format '{{.Id}}')"
image_ref="$(docker image inspect "${image_tag}" --format '{{index .RepoDigests 0}}')"
image_commit="$(
  docker image inspect "${image_tag}" --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'
)"
version_line="$(
  docker run --rm --entrypoint /usr/local/bin/xg2g "${image_ref}" --version
)"
version="$(awk '{print $1}' <<<"${version_line}")"
version_commit="$(sed -n 's/.*(commit: \([0-9a-fA-F]\{7,40\}\), built:.*/\1/p' <<<"${version_line}")"

[[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]]
[[ "${image_ref}" =~ ^[a-z0-9./_-]+@sha256:[0-9a-f]{64}$ ]]
[[ "${image_commit}" == "${expected_commit}" ]]
[[ "${version_commit}" == "${expected_commit}" ]]
[[ "${version#v}" == "${release_ref#v}" ]]

rm -f "${next_compose}" "${next_manifest}"
awk -v image="${image_ref}" -v binary="${binary}" '
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
    if (image_count != 1 || mount_count > 1 || port_count != 1) {
      print "ERROR: staging compose does not match the governed topology" > "/dev/stderr"
      exit 1
    }
  }
' "${compose_file}" >"${next_compose}"
mv "${next_compose}" "${compose_file}"
rm -f "${candidate_overlay}"

compose_args=(--project-directory /srv/xg2g-staging -f "${compose_file}")
[[ ! -f "${storage_overlay}" ]] || compose_args+=(-f "${storage_overlay}")
docker compose "${compose_args[@]}" up -d --force-recreate
wait_healthy || {
  docker logs --tail 100 xg2g-staging >&2 || true
  echo "ERROR: release candidate did not become healthy" >&2
  exit 1
}

running_version="$(docker exec xg2g-staging /usr/local/bin/xg2g --version)"
running_sha="$(docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
running_image_id="$(docker inspect --format '{{.Image}}' xg2g-staging)"
running_mount="$(
  docker inspect --format '{{range .Mounts}}{{if eq .Destination "/usr/local/bin/xg2g"}}{{println .Source}}{{end}}{{end}}' xg2g-staging |
    awk 'NF'
)"
[[ "${running_version}" == "${version_line}" ]]
[[ "${running_image_id}" == "${image_id}" ]]
[[ -z "${running_mount}" ]]

{
  printf 'schema=2\n'
  printf 'mode=candidate\n'
  printf 'source=release\n'
  printf 'version=%s\n' "${version}"
  printf 'commit=%s\n' "${expected_commit}"
  printf 'sha256=%s\n' "${running_sha}"
  printf 'image_id=%s\n' "${image_id}"
  printf 'image_ref=%s\n' "${image_ref}"
  printf 'deployed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"${next_manifest}"
mv "${next_manifest}" "${manifest}"

printf 'Release candidate staged: version=%s commit=%s image=%s port=8089\n' \
  "${version}" "${expected_commit}" "${image_ref}"
REMOTE

"${ROOT}/scripts/check-deployment-state.sh"
