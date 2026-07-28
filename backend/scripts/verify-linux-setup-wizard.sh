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
  "${SETUP}" --non-interactive --no-start --ref HEAD --install-root "${shared_root}" >/dev/null

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

if "${SETUP}" --non-interactive --no-start --ref HEAD --install-root "${shared_root}" >/dev/null 2>&1; then
  fail "existing env must not be overwritten without --keep-existing"
fi
"${SETUP}" --non-interactive --keep-existing --no-start --ref HEAD \
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
  "${SETUP}" --non-interactive --no-start --ref HEAD --install-root "${proxy_root}" >/dev/null

proxy_env="${proxy_root}/etc/xg2g/xg2g.env"
assert_contains "${proxy_env}" "XG2G_ALLOWED_ORIGINS='https://tv.example.test'"
assert_contains "${proxy_env}" "XG2G_TRUSTED_PROXIES='127.0.0.1/32,::1/128'"
assert_contains "${proxy_env}" '"kind":"local_https"'

invalid_root="$(mktemp -d)"
TEMP_DIRS+=("${invalid_root}")
if XG2G_SETUP_E2_HOST="http://192.0.2.10" \
  XG2G_SETUP_ACCESS_MODE="local" \
  XG2G_SETUP_STORAGE_MODE="dedicated" \
  XG2G_SETUP_HLS_ROOT="relative/path" \
  XG2G_SETUP_GPU_MODE="none" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --install-root "${invalid_root}" >/dev/null 2>&1; then
  fail "relative dedicated storage path must fail"
fi
[[ ! -e "${invalid_root}/etc/xg2g/xg2g.env" ]] || fail "failed validation must not write an env file"

echo "OK: guided Linux setup contract holds."
