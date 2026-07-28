#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-xg2g-dev}"
REMOTE_SOURCE_ROOT="${XG2G_DEPLOY_SOURCE_ROOT:-/srv/xg2g}"
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
[[ "${commit}" == "${origin_commit}" ]] || die "HEAD must exactly match pushed origin/${branch} before deployment"

echo "Preparing commit ${commit} in ${REMOTE_HOST}:${REMOTE_BUILD_ROOT}..."
ssh "${REMOTE_HOST}" bash -s -- "${REMOTE_SOURCE_ROOT}" "${REMOTE_BUILD_ROOT}" "${branch}" "${commit}" <<'REMOTE'
set -euo pipefail
source_root="$1"
build_root="$2"
branch="$3"
commit="$4"

origin_url="$(git -C "${source_root}" remote get-url origin 2>/dev/null || echo "https://github.com/ManuGH/xg2g.git")"
if [[ ! -d "${build_root}/.git" ]]; then
  if [[ -e "${build_root}" ]]; then
    rm -rf "${build_root}"
  fi
  git clone "${origin_url}" "${build_root}"
fi

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
expected_sha="$(ssh "${REMOTE_HOST}" "sha256sum '${remote_binary}' | awk '{print \$1}'")"
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
[[ "${running_sha}" == "${expected_sha}" ]] || {
  echo "ERROR: running staging hash ${running_sha} != ${expected_sha}" >&2
  exit 1
}
if [[ -n "${hls_root}" ]]; then
  running_hls_root="$(docker exec xg2g-staging printenv XG2G_HLS_ROOT)"
  [[ "${running_hls_root}" == "${hls_root}" ]] || {
    echo "ERROR: running staging HLS root ${running_hls_root} != ${hls_root}" >&2
    exit 1
  }
fi
printf '%s %s\n' "${commit}" "${expected_sha}" > /srv/xg2g-staging/deploy-manifest
REMOTE

echo "Staging deployment complete: commit=${commit} sha256=${expected_sha} port=8089"
echo "Production :8088 was not touched."
