#!/usr/bin/env bash
set -euo pipefail

REMOTE_HOST="${XG2G_DEPLOY_HOST:-root@10.10.55.2}"
CTID="${XG2G_DEPLOY_CTID:-110}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ "${1:-}" == "--confirm-production" ]] || die "explicit confirmation required: $0 --confirm-production"

ssh "${REMOTE_HOST}" bash -s -- "${CTID}" <<'REMOTE'
set -euo pipefail
ctid="$1"
staging_binary="/srv/xg2g-staging/xg2g-staging-binary"
rollback_armed=0
old_sha=""

production_binary="$(
  pct exec "${ctid}" -- docker inspect --format '{{range .Mounts}}{{if eq .Destination "/usr/local/bin/xg2g"}}{{println .Source}}{{end}}{{end}}' xg2g |
    awk 'NF'
)"
[[ "${production_binary}" =~ ^/srv/xg2g/xg2g([A-Za-z0-9._-]*)$ ]] || {
  echo "ERROR: production container has no trusted /usr/local/bin/xg2g host mount: ${production_binary:-missing}" >&2
  exit 1
}
pct exec "${ctid}" -- test -f "${production_binary}"
rollback_binary="${production_binary}.rollback"
next_binary="${production_binary}.next"
echo "Production binary mount resolved: ${production_binary}"

wait_ready() {
  local port="$1" container="$2" i status
  for ((i = 0; i < 90; i++)); do
    status="$(pct exec "${ctid}" -- docker inspect --format '{{.State.Health.Status}}' "${container}" 2>/dev/null || true)"
    if [[ "${status}" == "healthy" ]] && pct exec "${ctid}" -- curl -fsS "http://127.0.0.1:${port}/healthz" >/dev/null; then
      return 0
    fi
    [[ "${status}" != "unhealthy" ]] || return 1
    sleep 1
  done
  return 1
}

rollback() {
  echo "Production promotion failed; restoring ${old_sha}..." >&2
  pct exec "${ctid}" -- test -f "${rollback_binary}"
  pct exec "${ctid}" -- cp -f "${rollback_binary}" "${production_binary}"
  pct exec "${ctid}" -- chmod 0755 "${production_binary}"
  pct exec "${ctid}" -- systemctl restart xg2g
  wait_ready 8088 xg2g
  restored_sha="$(pct exec "${ctid}" -- docker exec xg2g sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
  [[ "${restored_sha}" == "${old_sha}" ]]
  echo "Rollback verified: sha256=${restored_sha}" >&2
}

on_exit() {
  status=$?
  trap - EXIT
  if [[ "${status}" != "0" && "${rollback_armed}" == "1" ]]; then
    if ! rollback; then
      echo "CRITICAL: automatic production rollback failed" >&2
      exit 2
    fi
  fi
  exit "${status}"
}
trap on_exit EXIT

wait_ready 8089 xg2g-staging
read -r manifest_commit manifest_sha < <(pct exec "${ctid}" -- cat /srv/xg2g-staging/deploy-manifest)
[[ -n "${manifest_commit}" && -n "${manifest_sha}" ]]
staging_sha="$(pct exec "${ctid}" -- docker exec xg2g-staging sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
file_sha="$(pct exec "${ctid}" -- sha256sum "${staging_binary}" | awk '{print $1}')"
[[ "${manifest_sha}" == "${staging_sha}" && "${manifest_sha}" == "${file_sha}" ]]

old_sha="$(pct exec "${ctid}" -- sha256sum "${production_binary}" | awk '{print $1}')"
pct exec "${ctid}" -- cp -f "${production_binary}" "${rollback_binary}"
rollback_armed=1
pct exec "${ctid}" -- systemctl stop xg2g
pct exec "${ctid}" -- cp -f "${staging_binary}" "${next_binary}"
pct exec "${ctid}" -- chmod 0755 "${next_binary}"
pct exec "${ctid}" -- mv "${next_binary}" "${production_binary}"
installed_sha="$(pct exec "${ctid}" -- sha256sum "${production_binary}" | awk '{print $1}')"
[[ "${installed_sha}" == "${manifest_sha}" ]]
pct exec "${ctid}" -- systemctl start xg2g
wait_ready 8088 xg2g
running_sha="$(pct exec "${ctid}" -- docker exec xg2g sha256sum /usr/local/bin/xg2g | awk '{print $1}')"
[[ "${running_sha}" == "${manifest_sha}" ]]

rollback_armed=0
pct exec "${ctid}" -- rm -f "${rollback_binary}"
echo "Production promotion complete: commit=${manifest_commit} sha256=${running_sha}"
REMOTE
