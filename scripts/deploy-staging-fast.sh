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
origin_url="$(git remote get-url origin)"
[[ "${commit}" == "${origin_commit}" ]] || die "HEAD must exactly match pushed origin/${branch} before deployment"
[[ -n "${origin_url}" ]] || die "origin URL could not be resolved"

echo "Preparing commit ${commit} in ${REMOTE_HOST}:${REMOTE_BUILD_ROOT}..."
ssh "${REMOTE_HOST}" bash -s -- "${REMOTE_BUILD_ROOT}" "${origin_url}" "${branch}" "${commit}" <<'REMOTE'
set -euo pipefail
build_root="$1"
origin_url="$2"
branch="$3"
commit="$4"

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
expected_sha="$(
  ssh "${REMOTE_HOST}" bash -s -- "${remote_binary}" <<'REMOTE'
set -euo pipefail
sha256sum "$1" | awk '{print $1}'
REMOTE
)"
[[ -n "${expected_sha}" ]] || die "could not hash remote build artifact"

echo "Deploying ${commit} (${expected_sha}) to staging :8089 on ${REMOTE_HOST}..."
ssh "${REMOTE_HOST}" bash -s -- "${remote_binary}" "${expected_sha}" "${commit}" <<'REMOTE'
set -euo pipefail
binary="$1"
expected_sha="$2"
commit="$3"
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

cp "${binary}" "${next}"
chmod 0755 "${next}"
mv "${next}" "${destination}"

compose_args=(--project-directory /srv/xg2g-staging -f "${compose_file}")
cat >"${candidate_overlay}.next" <<EOF
services:
  xg2g:
    volumes:
      - type: bind
        source: '${destination}'
        target: /usr/local/bin/xg2g
        read_only: true
EOF
mv "${candidate_overlay}.next" "${candidate_overlay}"
compose_args+=(-f "${candidate_overlay}")
hls_root="$(read_env_value XG2G_HLS_ROOT 2>/dev/null || true)"
require_mount="$(read_env_value XG2G_HLS_REQUIRE_MOUNT 2>/dev/null || true)"
case "${require_mount,,}" in
  ""|0|1|false|true|no|yes|off|on) ;;
  *)
    echo "ERROR: staging XG2G_HLS_REQUIRE_MOUNT must be a boolean, got: ${require_mount}" >&2
    exit 1
    ;;
esac
if [[ -n "${hls_root}" && "${hls_root}" != /var/lib/xg2g/* && "${hls_root}" != "/var/lib/xg2g" ]]; then
  [[ "${hls_root}" =~ ^/[A-Za-z0-9._/@+-]+$ ]] || {
    echo "ERROR: staging XG2G_HLS_ROOT must be a safe absolute Linux path: ${hls_root}" >&2
    exit 1
  }
  [[ -d "${hls_root}" && -w "${hls_root}" ]] || {
    echo "ERROR: staging HLS/DVR scratch path must exist and be writable: ${hls_root}" >&2
    exit 1
  }
  if [[ "${require_mount,,}" =~ ^(1|true|yes|on)$ ]]; then
    data_mount="$(findmnt -T /var/lib/xg2g-staging -n -o TARGET)"
    hls_mount="$(findmnt -T "${hls_root}" -n -o TARGET)"
    [[ -n "${data_mount}" && -n "${hls_mount}" && "${data_mount}" != "${hls_mount}" ]] || {
      echo "ERROR: staging requires a dedicated HLS mount, but data and DVR resolve to ${data_mount:-unknown}" >&2
      exit 1
    }
  fi
  cat > "${storage_overlay}" <<EOF
services:
  xg2g:
    volumes:
      - type: bind
        source: '${hls_root}'
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
  printf 'source=github\n'
  printf 'version=%s\n' "${running_version}"
  printf 'commit=%s\n' "${commit}"
  printf 'sha256=%s\n' "${expected_sha}"
  printf 'image_id=%s\n' "${running_image_id}"
  printf 'deployed_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"${manifest_next}"
mv "${manifest_next}" /srv/xg2g-staging/deploy-manifest
REMOTE

echo "Staging deployment complete: commit=${commit} sha256=${expected_sha} port=8089"
echo "Production :8088 was not touched."
"${ROOT}/scripts/check-deployment-state.sh"
