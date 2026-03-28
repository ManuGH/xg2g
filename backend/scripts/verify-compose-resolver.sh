#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
HELPER="${SCRIPT_DIR}/compose-xg2g.sh"
BASE_COMPOSE="${REPO_ROOT}/infrastructure/docker/docker-compose.yml"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local label="$3"

  if [[ "${actual}" != "${expected}" ]]; then
    echo "ERROR: ${label}" >&2
    echo "expected:" >&2
    printf '%s\n' "${expected}" >&2
    echo "actual:" >&2
    printf '%s\n' "${actual}" >&2
    exit 1
  fi
}

make_stack_root() {
  local root
  root="$(mktemp -d)"
  mkdir -p "${root}/bin"
  cat <<'EOF' > "${root}/docker-compose.yml"
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:test
EOF
  printf '%s\n' "${root}"
}

run_print_files() {
  local root="$1"
  local env_file="$2"

  XG2G_COMPOSE_ROOT="${root}" XG2G_ENV_FILE="${env_file}" "${HELPER}" --print-files
}

cleanup_roots=()
cleanup() {
  if [[ "${#cleanup_roots[@]}" -gt 0 ]]; then
    rm -rf "${cleanup_roots[@]}"
  fi
}
trap cleanup EXIT

root="$(make_stack_root)"
cleanup_roots+=("${root}")
actual="$(run_print_files "${root}" /dev/null)"
expected="${root}/docker-compose.yml"
assert_eq "${expected}" "${actual}" "base-only resolver output"

cat <<'EOF' > "${root}/docker-compose.gpu.yml"
services:
  xg2g:
    devices:
      - /dev/dri/renderD128:/dev/dri/renderD128
EOF
actual="$(run_print_files "${root}" /dev/null)"
expected="$(printf '%s\n%s' "${root}/docker-compose.yml" "${root}/docker-compose.gpu.yml")"
assert_eq "${expected}" "${actual}" "auto GPU overlay resolution"

cat <<'EOF' > "${root}/docker-compose.alt.yml"
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:alt
EOF
cat <<'EOF' > "${root}/docker-compose.extra.yml"
services:
  xg2g:
    environment:
      - XG2G_LOG_LEVEL=debug
EOF
cat <<EOF > "${root}/xg2g.env"
COMPOSE_FILE=docker-compose.alt.yml:docker-compose.extra.yml
EOF
actual="$(cd /tmp && run_print_files "${root}" "${root}/xg2g.env")"
expected="$(printf '%s\n%s' "${root}/docker-compose.alt.yml" "${root}/docker-compose.extra.yml")"
assert_eq "${expected}" "${actual}" "explicit COMPOSE_FILE resolution"

cat <<EOF > "${root}/xg2g-invalid.env"
COMPOSE_FILE=docker-compose.alt.yml:missing.yml
EOF
set +e
invalid_output="$(run_print_files "${root}" "${root}/xg2g-invalid.env" 2>&1)"
invalid_status=$?
set -e
if [[ "${invalid_status}" -eq 0 ]]; then
  fail "invalid COMPOSE_FILE unexpectedly succeeded"
fi
case "${invalid_output}" in
  *"compose file not found: ${root}/missing.yml"*) ;;
  *)
    fail "invalid COMPOSE_FILE did not report the missing file"
    ;;
esac

cat <<'EOF' > "${root}/bin/docker"
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "${XG2G_CAPTURE_FILE:?}"
EOF
chmod +x "${root}/bin/docker"
capture_file="${root}/docker-args.txt"
PATH="${root}/bin:${PATH}" \
XG2G_CAPTURE_FILE="${capture_file}" \
XG2G_COMPOSE_ROOT="${root}" \
XG2G_ENV_FILE="${root}/xg2g.env" \
XG2G_COMPOSE_PROJECT="resolver-test" \
  "${HELPER}" config -q
actual="$(cat "${capture_file}")"
expected="$(printf '%s\n' \
  "compose" \
  "--project-name" \
  "resolver-test" \
  "-f" \
  "${root}/docker-compose.alt.yml" \
  "-f" \
  "${root}/docker-compose.extra.yml" \
  "config" \
  "-q")"
assert_eq "${expected}" "${actual}" "docker compose argument ordering"

cat <<'EOF' > "${root}/bin/docker"
#!/usr/bin/env bash
set -euo pipefail
cat <<'OUT'
services:
  xg2g:
    environment:
      XG2G_API_TOKEN: abc123
      XG2G_DECISION_SECRET: supersecret
      XG2G_E2_HOST: http://root:pw@10.10.55.64
      XG2G_E2_PASS: boxsecret
      XG2G_LOG_LEVEL: debug
OUT
EOF
chmod +x "${root}/bin/docker"

actual="$(PATH="${root}/bin:${PATH}" \
XG2G_COMPOSE_ROOT="${root}" \
XG2G_ENV_FILE="${root}/xg2g.env" \
  "${HELPER}" config)"
case "${actual}" in
  *"XG2G_API_TOKEN: REDACTED"*) ;;
  *)
    fail "default config output did not redact XG2G_API_TOKEN"
    ;;
esac
case "${actual}" in
  *"XG2G_DECISION_SECRET: REDACTED"*) ;;
  *)
    fail "default config output did not redact XG2G_DECISION_SECRET"
    ;;
esac
case "${actual}" in
  *"XG2G_E2_PASS: REDACTED"*) ;;
  *)
    fail "default config output did not redact XG2G_E2_PASS"
    ;;
esac
case "${actual}" in
  *"XG2G_E2_HOST: http://REDACTED@10.10.55.64"*) ;;
  *)
    fail "default config output did not redact URL credentials"
    ;;
esac
case "${actual}" in
  *"XG2G_LOG_LEVEL: debug"*) ;;
  *)
    fail "default config output over-redacted non-secret values"
    ;;
esac

actual="$(PATH="${root}/bin:${PATH}" \
XG2G_COMPOSE_ROOT="${root}" \
XG2G_ENV_FILE="${root}/xg2g.env" \
XG2G_COMPOSE_CONFIG_REDACT=0 \
  "${HELPER}" config)"
case "${actual}" in
  *"XG2G_API_TOKEN: abc123"*"XG2G_DECISION_SECRET: supersecret"*"XG2G_E2_HOST: http://root:pw@10.10.55.64"*) ;;
  *)
    fail "raw config opt-out did not preserve secret values"
    ;;
esac

if grep -qE '^[[:space:]]*devices:[[:space:]]*$' "${BASE_COMPOSE}"; then
  fail "base compose contains a devices binding"
fi

if grep -q '/dev/dri/renderD' "${BASE_COMPOSE}"; then
  fail "base compose contains a GPU render node path"
fi

echo "OK: compose resolver contract holds."
