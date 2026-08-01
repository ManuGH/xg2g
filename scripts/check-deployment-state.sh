#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_RUNTIME_HOST:-xg2g-dev}"

# shellcheck source=scripts/lib/deployment-state.sh
source "${ROOT}/scripts/lib/deployment-state.sh"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

value_for() {
  local evidence="$1"
  local key="$2"
  sed -n "s/^${key}=//p" <<<"${evidence}" | tail -n 1
}

extract_commit() {
  sed -n 's/.*(commit: \([0-9a-fA-F]\{7,40\}\), built:.*/\1/p' <<<"$1"
}

extract_version() {
  awk '{print $1}' <<<"$1"
}

git -C "${ROOT}" fetch origin --tags --quiet 2>/dev/null ||
  die "could not refresh Git commit and tag evidence"

evidence="$(
  ssh -o BatchMode=yes -o ConnectTimeout=8 "${REMOTE_HOST}" bash -s <<'REMOTE'
set -euo pipefail

probe_environment() {
  local name="$1"
  local container="$2"
  local port="$3"
  local container_status health_status version_line binary_sha image_id binary_mount published

  container_status="$(docker inspect --format '{{.State.Status}}' "${container}" 2>/dev/null || true)"
  health_status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}missing{{end}}' "${container}" 2>/dev/null || true)"
  image_id="$(docker inspect --format '{{.Image}}' "${container}" 2>/dev/null || true)"
  binary_mount="$(
    docker inspect --format '{{range .Mounts}}{{if eq .Destination "/usr/local/bin/xg2g"}}{{println .Source}}{{end}}{{end}}' "${container}" 2>/dev/null |
      awk 'NF' || true
  )"
  published="$(docker port "${container}" "${port}/tcp" 2>/dev/null | paste -sd, - || true)"
  version_line="$(docker exec "${container}" /usr/local/bin/xg2g --version 2>/dev/null || true)"
  binary_sha="$(docker exec "${container}" sha256sum /usr/local/bin/xg2g 2>/dev/null | awk '{print $1}' || true)"

  printf '%s.container=%s\n' "${name}" "${container_status:-missing}"
  printf '%s.health=%s\n' "${name}" "${health_status:-missing}"
  printf '%s.image_id=%s\n' "${name}" "${image_id:-missing}"
  printf '%s.binary_mount=%s\n' "${name}" "${binary_mount}"
  printf '%s.published=%s\n' "${name}" "${published:-missing}"
  if curl -fsS --max-time 5 "http://127.0.0.1:${port}/healthz" >/dev/null; then
    printf '%s.endpoint=healthy\n' "${name}"
  else
    printf '%s.endpoint=unhealthy\n' "${name}"
  fi
  printf '%s.version_line=%s\n' "${name}" "${version_line:-unavailable}"
  printf '%s.binary_sha256=%s\n' "${name}" "${binary_sha:-unavailable}"
}

manifest_value() {
  local key="$1"
  sed -n "s/^${key}=//p" /srv/xg2g-staging/deploy-manifest 2>/dev/null | tail -n 1
}

probe_environment production xg2g 8088
probe_environment staging xg2g-staging 8089

if [[ "$(manifest_value schema)" == "2" ]]; then
  printf 'manifest.mode=%s\n' "$(manifest_value mode)"
  printf 'manifest.commit=%s\n' "$(manifest_value commit)"
  printf 'manifest.sha256=%s\n' "$(manifest_value sha256)"
else
  read -r legacy_commit legacy_sha </srv/xg2g-staging/deploy-manifest 2>/dev/null || true
  printf 'manifest.mode=legacy\n'
  printf 'manifest.commit=%s\n' "${legacy_commit:-missing}"
  printf 'manifest.sha256=%s\n' "${legacy_sha:-missing}"
fi
REMOTE
)" || die "could not collect deployment evidence from ${REMOTE_HOST}"

production_container="$(value_for "${evidence}" production.container)"
production_health="$(value_for "${evidence}" production.health)"
production_endpoint="$(value_for "${evidence}" production.endpoint)"
production_version_line="$(value_for "${evidence}" production.version_line)"
production_sha="$(value_for "${evidence}" production.binary_sha256)"
production_image_id="$(value_for "${evidence}" production.image_id)"
production_published="$(value_for "${evidence}" production.published)"
staging_container="$(value_for "${evidence}" staging.container)"
staging_health="$(value_for "${evidence}" staging.health)"
staging_endpoint="$(value_for "${evidence}" staging.endpoint)"
staging_version_line="$(value_for "${evidence}" staging.version_line)"
staging_sha="$(value_for "${evidence}" staging.binary_sha256)"
staging_image_id="$(value_for "${evidence}" staging.image_id)"
staging_binary_mount="$(value_for "${evidence}" staging.binary_mount)"
staging_published="$(value_for "${evidence}" staging.published)"
manifest_mode="$(value_for "${evidence}" manifest.mode)"
manifest_commit="$(value_for "${evidence}" manifest.commit)"
manifest_sha="$(value_for "${evidence}" manifest.sha256)"

printf 'production.role=production\n'
printf 'production.port=8088\n'
printf 'production.version=%s\n' "$(extract_version "${production_version_line}")"
printf 'production.commit=%s\n' "$(extract_commit "${production_version_line}")"
printf 'production.binary_sha256=%s\n' "${production_sha}"
printf 'production.image_id=%s\n' "${production_image_id}"
printf 'production.published=%s\n' "${production_published}"
printf 'production.health=%s/%s/%s\n' "${production_container}" "${production_health}" "${production_endpoint}"
printf 'staging.role=staging\n'
printf 'staging.port=8089\n'
printf 'staging.version=%s\n' "$(extract_version "${staging_version_line}")"
printf 'staging.commit=%s\n' "$(extract_commit "${staging_version_line}")"
printf 'staging.binary_sha256=%s\n' "${staging_sha}"
printf 'staging.image_id=%s\n' "${staging_image_id}"
printf 'staging.binary_override=%s\n' "${staging_binary_mount:-none}"
printf 'staging.published=%s\n' "${staging_published}"
printf 'staging.health=%s/%s/%s\n' "${staging_container}" "${staging_health}" "${staging_endpoint}"
printf 'staging.manifest_mode=%s\n' "${manifest_mode}"

[[ "${production_container}/${production_health}/${production_endpoint}" == "running/healthy/healthy" ]] ||
  die "production is not fully healthy"
[[ "${staging_container}/${staging_health}/${staging_endpoint}" == "running/healthy/healthy" ]] ||
  die "staging is not fully healthy"
[[ "${production_published}" == 127.0.0.1:* || "${production_published}" == "[::1]:"* ]] ||
  die "production port 8088 is not loopback-only"
[[ "${staging_published}" == *127.0.0.1:* || "${staging_published}" == *"[::1]:"* || "${staging_published}" == *10.10.55.14:* ]] ||
  die "staging port 8089 is not loopback-only"
[[ "${production_sha}" =~ ^[0-9a-f]{64}$ ]] || die "production binary hash is unavailable"
[[ "${staging_sha}" =~ ^[0-9a-f]{64}$ ]] || die "staging binary hash is unavailable"

production_commit="$(extract_commit "${production_version_line}")"
staging_commit="$(extract_commit "${staging_version_line}")"
[[ -n "${production_commit}" ]] || die "production commit metadata is unavailable"
[[ -n "${staging_commit}" ]] || die "staging commit metadata is unavailable"

state="$(
  classify_deployment_state \
    "${ROOT}" \
    "${production_commit}" \
    "${production_sha}" \
    "${staging_commit}" \
    "${staging_sha}" \
    "${manifest_mode}" \
    "${manifest_commit}" \
    "${manifest_sha}" \
    "${production_image_id}" \
    "${staging_image_id}" \
    "${staging_binary_mount}"
)" || {
  printf 'deployment.state=%s\n' "${state:-unknown}"
  die "invalid production/staging lifecycle state"
}

printf 'deployment.state=%s\n' "${state}"
