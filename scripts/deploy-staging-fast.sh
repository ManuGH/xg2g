#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-xg2g-dev}"
REMOTE_BUILD_ROOT="${XG2G_DEPLOY_BUILD_ROOT:-/srv/xg2g-build}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

if [[ "${1:-}" != "--confirm-staging" ]]; then
  die "staging deployment requires explicit confirmation: ./scripts/fast_deploy.sh --confirm-staging [--full-image]"
fi
shift
# binary: the Go binary is bind-mounted over the staging base image, whose
# xg2g-media-core stays as it is. full-image: the whole runtime image - Go,
# media-core and WebUI - is built from the same commit and run as the candidate.
deploy_mode="binary"
if [[ "${1:-}" == "--full-image" ]]; then
  deploy_mode="full-image"
  shift
fi
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
origin_url="$(git remote get-url origin)"
[[ "${commit}" == "${origin_commit}" ]] || die "HEAD must exactly match pushed origin/${branch} before deployment"
[[ -n "${origin_url}" ]] || die "origin URL could not be resolved"

# The Go daemon and xg2g-media-core speak one wire protocol, checked at the
# handshake and fatal when it differs - but the handshake happens when the first
# live pipeline starts, long after /healthz is green. So the pairing is checked
# here, before anything is built or changed.
wire_go="${ROOT}/backend/internal/stream/ingest/remotecore/wire.go"
go_protocol="$(sed -n 's/^const Version uint8 = \([0-9][0-9]*\)$/\1/p' "${wire_go}")"
[[ "${go_protocol}" =~ ^[0-9]+$ ]] || die "could not read the media-core wire version from ${wire_go#"${ROOT}"/}"

if [[ "${deploy_mode}" == "binary" ]]; then
  # A binary deploy keeps the base image's media-core. Refuse unless that core
  # states the protocol this Go build speaks; a base image whose core is missing
  # or predates --protocol-version cannot be paired safely.
  base_protocol="$(
    ssh "${REMOTE_HOST}" bash -s <<'REMOTE'
set -euo pipefail
base_image="$(docker compose --project-directory /srv/xg2g-staging -f /srv/xg2g-staging/docker-compose.yml config --images | head -n 1)"
docker run --rm --entrypoint /usr/local/bin/xg2g-media-core "${base_image}" --protocol-version 2>/dev/null || true
REMOTE
  )"
  [[ "${base_protocol}" == "${go_protocol}" ]] ||
    die "this commit's Go daemon speaks media-core protocol v${go_protocol}; the staging base image's media-core states '${base_protocol:-nothing}'. A binary deploy would break live playback - use --full-image"
fi

echo "Preparing commit ${commit} in ${REMOTE_HOST}:${REMOTE_BUILD_ROOT}..."
ssh "${REMOTE_HOST}" bash -s -- "${REMOTE_BUILD_ROOT}" "${origin_url}" "${branch}" "${commit}" "${deploy_mode}" <<'REMOTE'
set -euo pipefail
build_root="$1"
origin_url="$2"
branch="$3"
commit="$4"
deploy_mode="$5"

if [[ ! -d "${build_root}/.git" ]]; then
  [[ ! -e "${build_root}" ]] || {
    echo "ERROR: ${build_root} exists but is not a Git checkout" >&2
    exit 1
  }
  git clone "${origin_url}" "${build_root}"
fi

# This script's own `make build-with-ui` regenerates the committed, content-hashed
# WebUI bundle, so every successful deploy leaves that path modified/deleted/untracked
# and the guard below would block the next run. Reset just the generated path back to
# HEAD first; `ui-build` rebuilds it from scratch anyway. Every other path stays under
# the strict guard, so real local work on the build host is still protected.
generated_dist="backend/internal/control/http/dist"
if [[ -d "${build_root}/${generated_dist}" ]] &&
  [[ -n "$(git -C "${build_root}" status --porcelain=v1 -uall -- "${generated_dist}")" ]]; then
  echo "Discarding regenerated WebUI bundle in ${generated_dist}:"
  git -C "${build_root}" status --porcelain=v1 -uall -- "${generated_dist}"
  git -C "${build_root}" checkout -- "${generated_dist}"
  git -C "${build_root}" clean -fdq -- "${generated_dist}"
fi

[[ -z "$(git -C "${build_root}" status --porcelain=v1 -uall)" ]] || {
  echo "ERROR: Linux build checkout is dirty; refusing to overwrite it" >&2
  git -C "${build_root}" status --porcelain=v1 -uall >&2
  exit 1
}

cd "${build_root}"
git remote set-url origin "${origin_url}"
git fetch origin "${branch}" --quiet
[[ "$(git rev-parse "origin/${branch}")" == "${commit}" ]] || {
  echo "ERROR: remote origin/${branch} does not match requested commit ${commit}" >&2
  exit 1
}
git switch --detach "${commit}"
[[ "$(git rev-parse HEAD)" == "${commit}" ]] || exit 1

if [[ "${deploy_mode}" == "full-image" ]]; then
  # The Dockerfile builds the WebUI, the Go daemon and media-core itself, so the
  # checkout is only read. The tag names the commit; the image ID is what deploys.
  base_version="$(tr -d '[:space:]' < backend/VERSION)"
  [[ "${base_version}" =~ ^v[0-9]+[.][0-9]+[.][0-9]+$ ]] || {
    echo "ERROR: invalid backend/VERSION: ${base_version}" >&2
    exit 1
  }
  docker build \
    --tag "xg2g:staging-${commit:0:8}" \
    --build-arg BUILD_VERSION="${base_version}-staging.${commit:0:8}" \
    --build-arg BUILD_COMMIT="${commit}" \
    --build-arg BUILD_DATE="$(git show -s --format=%cI "${commit}")" \
    --label org.opencontainers.image.revision="${commit}" \
    .
  exit 0
fi

node_version="$(tr -d '[:space:]' < .node-version)"
[[ "${node_version}" =~ ^[0-9]+([.][0-9]+){0,2}$ ]] || {
  echo "ERROR: invalid .node-version: ${node_version}" >&2
  exit 1
}
if command -v fnm >/dev/null 2>&1; then
  fnm install "${node_version}" >/dev/null
  echo "Using $(fnm exec --using "${node_version}" node --version) for WebUI build"
  fnm exec --using "${node_version}" make build-with-ui
else
  actual_node_major="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || true)"
  [[ "${actual_node_major}" == "${node_version%%.*}" ]] || {
    echo "ERROR: Node ${node_version} is required, found $(node --version 2>/dev/null || echo unavailable)" >&2
    exit 1
  }
  make build-with-ui
fi
REMOTE

remote_binary="${REMOTE_BUILD_ROOT}/bin/xg2g"
image_id=""
expected_media_core_sha=""
if [[ "${deploy_mode}" == "full-image" ]]; then
  image_evidence="$(
    ssh "${REMOTE_HOST}" bash -s -- "xg2g:staging-${commit:0:8}" <<'REMOTE'
set -euo pipefail
image="$1"
printf 'image_id=%s\n' "$(docker image inspect --format '{{.Id}}' "${image}")"
docker run --rm --entrypoint sha256sum "${image}" /usr/local/bin/xg2g /usr/local/bin/xg2g-media-core |
  awk '{ sub(".*/", "", $2); printf "sha256.%s=%s\n", $2, $1 }'
printf 'protocol=%s\n' "$(docker run --rm --entrypoint /usr/local/bin/xg2g-media-core "${image}" --protocol-version)"
REMOTE
  )"
  image_id="$(sed -n 's/^image_id=//p' <<<"${image_evidence}")"
  expected_sha="$(sed -n 's/^sha256[.]xg2g=//p' <<<"${image_evidence}")"
  expected_media_core_sha="$(sed -n 's/^sha256[.]xg2g-media-core=//p' <<<"${image_evidence}")"
  image_protocol="$(sed -n 's/^protocol=//p' <<<"${image_evidence}")"
  [[ "${image_id}" =~ ^sha256:[0-9a-f]{64}$ ]] || die "could not identify the built image"
  [[ "${expected_media_core_sha}" =~ ^[0-9a-f]{64}$ ]] || die "could not hash the image's media-core"
  [[ "${image_protocol}" == "${go_protocol}" ]] ||
    die "the built image's media-core speaks protocol '${image_protocol}', its Go daemon v${go_protocol}"
else
  expected_sha="$(
    ssh "${REMOTE_HOST}" bash -s -- "${remote_binary}" <<'REMOTE'
set -euo pipefail
sha256sum "$1" | awk '{print $1}'
REMOTE
  )"
fi
[[ "${expected_sha}" =~ ^[0-9a-f]{64}$ ]] || die "could not hash remote build artifact"

echo "Deploying ${commit} (${expected_sha}) to staging :8089 on ${REMOTE_HOST}..."
# The storage preflight below is the same file the canonical Compose helper
# uses; it is prepended to the remote script so both paths run one shared
# implementation rather than two copies that can drift.
storage_lib="${ROOT}/backend/scripts/lib/hls-storage.sh"
[[ -r "${storage_lib}" ]] || die "shared storage preflight missing: ${storage_lib}"
{
cat "${storage_lib}"
cat <<'REMOTE'
set -euo pipefail
binary="$1"
expected_sha="$2"
commit="$3"
deploy_mode="$4"
image_id="$5"
expected_media_core_sha="$6"
go_protocol="$7"
next="/srv/xg2g-staging/xg2g-staging-binary.next"
destination="/srv/xg2g-staging/xg2g-staging-binary"
compose_file="/srv/xg2g-staging/docker-compose.yml"
storage_overlay="/srv/xg2g-staging/docker-compose.storage.yml"
candidate_overlay="/srv/xg2g-staging/docker-compose.candidate.yml"
env_file="/etc/xg2g/xg2g-staging.env"

read_env_value() {
  local key="$1"
  awk -F= -v wanted="${key}" '
    /^[[:space:]]*#/ { next }
    {
      key = $1
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", key)
      sub(/^export[[:space:]]+/, "", key)
      if (key != wanted) next
      value = substr($0, index($0, "=") + 1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", value)
      if (value ~ /^".*"$/ || value ~ /^'\''.*'\''$/) {
        value = substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "${env_file}"
}

compose_args=(--project-directory /srv/xg2g-staging -f "${compose_file}")
if [[ "${deploy_mode}" == "full-image" ]]; then
  # The candidate is the image itself: no binary is mounted over it, so what runs
  # is exactly what was built, media-core included.
  cat >"${candidate_overlay}.next" <<EOF
services:
  xg2g:
    image: ${image_id}
EOF
else
  cp "${binary}" "${next}"
  chmod 0755 "${next}"
  mv "${next}" "${destination}"
  cat >"${candidate_overlay}.next" <<EOF
services:
  xg2g:
    volumes:
      - type: bind
        source: '${destination}'
        target: /usr/local/bin/xg2g
        read_only: true
EOF
fi
mv "${candidate_overlay}.next" "${candidate_overlay}"
compose_args+=(-f "${candidate_overlay}")
hls_root="$(read_env_value XG2G_HLS_ROOT 2>/dev/null || true)"
require_mount="$(read_env_value XG2G_HLS_REQUIRE_MOUNT 2>/dev/null || true)"
data_root="$(read_env_value XG2G_DATA 2>/dev/null || true)"
: "${data_root:=/var/lib/xg2g}"
: "${hls_root:=${data_root%/}/hls}"
data_root_host="/var/lib/xg2g-staging"

# XG2G_DATA and XG2G_HLS_ROOT are container paths; the mount topology that
# decides whether DVR scratch can fill the root filesystem is the host's.
# Resolve container paths onto the host before validating, so the check sees
# the filesystem the container is actually about to be handed.
if [[ "${hls_root}" == "${data_root}" || "${hls_root}" == "${data_root%/}/"* ]]; then
  hls_root_host="${data_root_host}${hls_root#"${data_root}"}"
else
  hls_root_host="${hls_root}"
fi

echo "Staging storage topology (host view):"
xg2g_hls_report_topology "${data_root_host}" "${hls_root_host}" "${require_mount}"

# Unconditional: this runs for every configuration, including an HLS root
# nested inside the data root. Nesting it behind an "is the path external"
# test is what let XG2G_HLS_REQUIRE_MOUNT=true pass while HLS scratch was
# being written to the staging root filesystem.
xg2g_hls_validate_storage "${data_root_host}" "${hls_root_host}" "${require_mount}" || {
  echo "ERROR: staging storage preflight failed; no container was changed" >&2
  exit 1
}

# Only an HLS root that is not already covered by the data bind mount needs a
# bind mount of its own. This is a mount-wiring question, not a safety one --
# the safety question was answered above, for every configuration.
if [[ "${hls_root_host}" != "${data_root_host}" && "${hls_root_host}" != "${data_root_host}/"* ]]; then
  cat > "${storage_overlay}" <<EOF
services:
  xg2g:
    volumes:
      - type: bind
        source: '${hls_root_host}'
        target: '${hls_root}'
EOF
  compose_args+=(-f "${storage_overlay}")
else
  rm -f "${storage_overlay}"
fi

docker compose "${compose_args[@]}" up -d --force-recreate

healthy=0
for ((i = 0; i < 90; i++)); do
  status="$(docker inspect --format '{{.State.Health.Status}}' xg2g-staging 2>/dev/null || true)"
  if [[ "${status}" == "healthy" ]] && curl -fsS http://127.0.0.1:8089/healthz >/dev/null; then
    healthy=1
    break
  fi
  [[ "${status}" != "unhealthy" ]] || break
  sleep 1
done
[[ "${healthy}" == "1" ]] || {
  docker logs --tail 100 xg2g-staging >&2 || true
  echo "ERROR: staging did not become healthy" >&2
  exit 1
}

running_sha="$(docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
running_image_id="$(docker inspect --format '{{.Image}}' xg2g-staging)"
[[ "${running_sha}" == "${expected_sha}" ]] || {
  echo "ERROR: running staging hash ${running_sha} != ${expected_sha}" >&2
  exit 1
}
running_media_core_sha="$(docker exec xg2g-staging sha256sum /usr/local/bin/xg2g-media-core | awk '{print $1}')"
running_protocol="$(docker exec xg2g-staging /usr/local/bin/xg2g-media-core --protocol-version)"
[[ "${running_protocol}" == "${go_protocol}" ]] || {
  echo "ERROR: running media-core speaks protocol ${running_protocol:-nothing}, the daemon v${go_protocol}" >&2
  exit 1
}
if [[ "${deploy_mode}" == "full-image" ]]; then
  [[ "${running_image_id}" == "${image_id}" ]] || {
    echo "ERROR: running staging image ${running_image_id} != ${image_id}" >&2
    exit 1
  }
  [[ "${running_media_core_sha}" == "${expected_media_core_sha}" ]] || {
    echo "ERROR: running media-core hash ${running_media_core_sha} != ${expected_media_core_sha}" >&2
    exit 1
  }
fi
version_line="$(docker exec xg2g-staging /usr/local/bin/xg2g --version)"
running_commit="$(sed -n 's/.*(commit: \([0-9a-fA-F]\{7,40\}\), built:.*/\1/p' <<<"${version_line}")"
running_version="$(awk '{print $1}' <<<"${version_line}")"
[[ "${commit}" == "${running_commit}"* || "${running_commit}" == "${commit}"* ]] || {
  echo "ERROR: running staging commit ${running_commit:-missing} != ${commit}" >&2
  exit 1
}
if [[ -n "${hls_root}" ]]; then
  running_hls_root="$(docker exec xg2g-staging printenv XG2G_HLS_ROOT)"
  [[ "${running_hls_root}" == "${hls_root}" ]] || {
    echo "ERROR: running staging HLS root ${running_hls_root} != ${hls_root}" >&2
    exit 1
  }
fi
manifest_next="/srv/xg2g-staging/deploy-manifest.next"
{
  printf 'schema=2\n'
  printf 'mode=candidate\n'
  if [[ "${deploy_mode}" == "full-image" ]]; then
    printf 'source=github-full-image\n'
  else
    printf 'source=github\n'
  fi
  printf 'version=%s\n' "${running_version}"
  printf 'commit=%s\n' "${commit}"
  printf 'sha256=%s\n' "${expected_sha}"
  printf 'image_id=%s\n' "${running_image_id}"
  printf 'media_core_sha256=%s\n' "${running_media_core_sha}"
  printf 'deployed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"${manifest_next}"
mv "${manifest_next}" /srv/xg2g-staging/deploy-manifest
REMOTE
} | ssh "${REMOTE_HOST}" bash -s -- "${remote_binary}" "${expected_sha}" "${commit}" "${deploy_mode}" "${image_id}" "${expected_media_core_sha}" "${go_protocol}"

echo "Staging deployment complete: mode=${deploy_mode} commit=${commit} sha256=${expected_sha} protocol=v${go_protocol} port=8089"
echo "Production :8088 was not touched."
"${ROOT}/scripts/check-deployment-state.sh"
