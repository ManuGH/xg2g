#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SETUP="${REPO_ROOT}/infra/systemd/setup-linux.sh"
TEMP_DIRS=()

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  local dir
  for dir in "${TEMP_DIRS[@]:-}"; do
    [[ -n "${dir}" && -d "${dir}" ]] && rm -rf "${dir}"
  done
  return 0
}
trap cleanup EXIT

assert_contains() {
  local file="$1"
  local expected="$2"
  grep -Fq -- "${expected}" "${file}" || fail "expected ${expected} in ${file}"
}

bash -n "${SETUP}"
help_output="$("${SETUP}" --help)"
grep -Fq -- "never partitions, formats, mounts, or deletes" <<< "${help_output}"

shared_root="$(mktemp -d)"
TEMP_DIRS+=("${shared_root}")
XG2G_SETUP_E2_HOST="http://192.0.2.10" \
XG2G_SETUP_E2_USER="tester" \
XG2G_SETUP_E2_PASS="pa'ss\\word\$literal#value" \
XG2G_SETUP_ACCESS_MODE="local" \
XG2G_SETUP_DVR_WINDOW="2h" \
XG2G_SETUP_STORAGE_MODE="shared" \
XG2G_SETUP_GPU_MODE="none" \
XG2G_SETUP_API_TOKEN="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
XG2G_SETUP_DECISION_SECRET="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" --install-root "${shared_root}" >/dev/null

shared_env="${shared_root}/etc/xg2g/xg2g.env"
[[ -f "${shared_env}" ]] || fail "shared setup did not create env file"
[[ "$(stat -c '%a' "${shared_env}" 2>/dev/null || stat -f '%Lp' "${shared_env}")" == "600" ]] ||
  fail "generated env file must have mode 0600"
# The literal expression is the governed source contract.
# shellcheck disable=SC2016
grep -Fq -- 'install -d -m 0750 -o 10001 -g 10001 "${data_path}"' "${SETUP}" ||
  fail "runtime data directory must match the non-root release image UID/GID"
assert_contains "${shared_env}" "XG2G_HLS_ROOT='/var/lib/xg2g/hls'"
assert_contains "${shared_env}" "XG2G_HLS_REQUIRE_MOUNT='false'"
assert_contains "${shared_env}" "XG2G_CONNECTIVITY_PROFILE='lan'"
assert_contains "${shared_env}" "XG2G_E2_PASS='pa\\'ss\\word\$literal#value'"
[[ -f "${shared_root}/etc/systemd/system/xg2g.service" ]] || fail "canonical sync was not applied"

if "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" --install-root "${shared_root}" >/dev/null 2>&1; then
  fail "existing env must not be overwritten without --keep-existing"
fi
"${SETUP}" --non-interactive --keep-existing --no-start --ref HEAD --source-dir "${REPO_ROOT}" \
  --install-root "${shared_root}" >/dev/null

proxy_root="$(mktemp -d)"
TEMP_DIRS+=("${proxy_root}")
XG2G_SETUP_E2_HOST="https://receiver.example.test" \
XG2G_SETUP_ACCESS_MODE="private_proxy" \
XG2G_SETUP_HTTPS_ORIGIN="https://tv.example.test" \
XG2G_SETUP_TRUSTED_PROXIES="127.0.0.1/32,::1/128" \
XG2G_SETUP_DVR_WINDOW="45m" \
XG2G_SETUP_STORAGE_MODE="shared" \
XG2G_SETUP_GPU_MODE="none" \
XG2G_SETUP_API_TOKEN="cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" \
XG2G_SETUP_DECISION_SECRET="dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" --install-root "${proxy_root}" >/dev/null

proxy_env="${proxy_root}/etc/xg2g/xg2g.env"
assert_contains "${proxy_env}" "XG2G_ALLOWED_ORIGINS='https://tv.example.test'"
assert_contains "${proxy_env}" "XG2G_TRUSTED_PROXIES='127.0.0.1/32,::1/128'"
assert_contains "${proxy_env}" '"kind":"local_https"'
[[ ! -e "${proxy_root}/etc/xg2g/Caddyfile" ]] ||
  fail "existing-proxy mode must not create a managed Caddyfile"
[[ ! -e "${proxy_root}/var/lib/xg2g-caddy" ]] ||
  fail "existing-proxy mode must not create managed Caddy state"

managed_proxy_root="$(mktemp -d)"
TEMP_DIRS+=("${managed_proxy_root}")
XG2G_SETUP_E2_HOST="http://192.0.2.10" \
XG2G_SETUP_ACCESS_MODE="private_proxy" \
XG2G_SETUP_HTTPS_ORIGIN="https://tv.home.example" \
XG2G_SETUP_TRUSTED_PROXIES="127.0.0.1/32,::1/128" \
XG2G_SETUP_PROXY_MODE="caddy_internal" \
XG2G_SETUP_CADDY_BIND_IP="10.8.0.1" \
XG2G_SETUP_STORAGE_MODE="shared" \
XG2G_SETUP_GPU_MODE="none" \
XG2G_SETUP_API_TOKEN="abababababababababababababababababababababababababababababababab" \
XG2G_SETUP_DECISION_SECRET="cdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcdcd" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" \
  --install-root "${managed_proxy_root}" >/dev/null
assert_contains "${managed_proxy_root}/etc/xg2g/Caddyfile" "tv.home.example {"
assert_contains "${managed_proxy_root}/etc/xg2g/Caddyfile" $'\ttls internal'
assert_contains "${managed_proxy_root}/etc/xg2g/Caddyfile" $'\tbind 10.8.0.1'
assert_contains "${managed_proxy_root}/etc/xg2g/Caddyfile" $'\treverse_proxy 127.0.0.1:8088'
[[ -f "${managed_proxy_root}/etc/systemd/system/xg2g-caddy.service" ]] ||
  fail "managed Caddy service was not installed"
assert_contains "${managed_proxy_root}/etc/systemd/system/xg2g-caddy.service" "caddy:2.11.4-alpine"

invalid_caddy_port_root="$(mktemp -d)"
TEMP_DIRS+=("${invalid_caddy_port_root}")
if XG2G_SETUP_E2_HOST="http://192.0.2.10" \
  XG2G_SETUP_ACCESS_MODE="private_proxy" \
  XG2G_SETUP_HTTPS_ORIGIN="https://tv.home.example:8443" \
  XG2G_SETUP_TRUSTED_PROXIES="127.0.0.1/32,::1/128" \
  XG2G_SETUP_PROXY_MODE="caddy_internal" \
  XG2G_SETUP_STORAGE_MODE="shared" \
  XG2G_SETUP_GPU_MODE="none" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" \
  --install-root "${invalid_caddy_port_root}" >/dev/null 2>&1; then
  fail "managed Caddy accepted an HTTPS origin on an unconfigured custom port"
fi

invalid_root="$(mktemp -d)"
TEMP_DIRS+=("${invalid_root}")
if XG2G_SETUP_E2_HOST="http://192.0.2.10" \
  XG2G_SETUP_ACCESS_MODE="local" \
  XG2G_SETUP_STORAGE_MODE="dedicated" \
  XG2G_SETUP_HLS_ROOT="relative/path" \
  XG2G_SETUP_GPU_MODE="none" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" --install-root "${invalid_root}" >/dev/null 2>&1; then
  fail "relative dedicated storage path must fail"
fi
[[ ! -e "${invalid_root}/etc/xg2g/xg2g.env" ]] || fail "failed validation must not write an env file"

# Exercise the path users get from an official GitHub release archive: no
# .git directory, a bundled daemon, and every canonical deployment input local.
archive_root="$(mktemp -d)"
archive_install_root="$(mktemp -d)"
TEMP_DIRS+=("${archive_root}" "${archive_install_root}")
mkdir -p \
  "${archive_root}/backend/scripts" \
  "${archive_root}/infra" \
  "${archive_root}/docs/man" \
  "${archive_root}/docs/ops"
cp -R "${REPO_ROOT}/infra/systemd" "${archive_root}/infra/systemd"
for helper in \
  compose-xg2g.sh \
  verify-compose-contract.sh \
  verify-installed-unit.sh \
  verify-systemd-runtime-contract.sh \
  verify-installation-contract.sh \
  verify-runtime.sh; do
  cp "${REPO_ROOT}/backend/scripts/${helper}" "${archive_root}/backend/scripts/${helper}"
done
cp "${REPO_ROOT}/backend/VERSION" "${archive_root}/backend/VERSION"
cp "${REPO_ROOT}/DIGESTS.lock" "${archive_root}/DIGESTS.lock"
cp "${REPO_ROOT}/docs/man/xg2g.1" "${archive_root}/docs/man/xg2g.1"
cp "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "${archive_root}/docs/ops/xg2g-verifier.service"
cp "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "${archive_root}/docs/ops/xg2g-verifier.timer"
printf '#!/usr/bin/env sh\nexit 0\n' > "${archive_root}/xg2g"
chmod 0755 "${archive_root}/xg2g"

XG2G_SETUP_E2_HOST="http://192.0.2.10" \
XG2G_SETUP_ACCESS_MODE="local" \
XG2G_SETUP_STORAGE_MODE="shared" \
XG2G_SETUP_GPU_MODE="none" \
XG2G_SETUP_API_TOKEN="eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee" \
XG2G_SETUP_DECISION_SECRET="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff" \
  "${archive_root}/infra/systemd/setup-linux.sh" \
  --non-interactive --no-start --install-root "${archive_install_root}" >/dev/null
[[ -f "${archive_install_root}/srv/xg2g/docker-compose.yml" ]] ||
  fail "official no-git release bundle did not install canonical Compose"

# A GitHub-generated branch source ZIP has no immutable release marker/binary.
# It must fail with guidance instead of silently installing backend/VERSION.
source_zip_root="$(mktemp -d)"
source_zip_install_root="$(mktemp -d)"
TEMP_DIRS+=("${source_zip_root}" "${source_zip_install_root}")
mkdir -p "${source_zip_root}/infra/systemd" "${source_zip_root}/backend"
cp "${REPO_ROOT}/infra/systemd/setup-linux.sh" "${source_zip_root}/infra/systemd/setup-linux.sh"
cp "${REPO_ROOT}/backend/VERSION" "${source_zip_root}/backend/VERSION"
source_zip_error="${source_zip_root}/error.txt"
if XG2G_SETUP_E2_HOST="http://192.0.2.10" \
  "${source_zip_root}/infra/systemd/setup-linux.sh" \
  --non-interactive --no-start --install-root "${source_zip_install_root}" > /dev/null 2> "${source_zip_error}"; then
  fail "mutable GitHub source ZIP must not guess an install ref"
fi
assert_contains "${source_zip_error}" "github.com/ManuGH/xg2g/releases"
[[ ! -e "${source_zip_install_root}/etc/xg2g/xg2g.env" ]] ||
  fail "rejected source ZIP must not write host configuration"

echo "OK: guided Linux setup contract holds."
