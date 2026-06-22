#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
DEV_COMPOSE="${SCRIPT_DIR}/dev-compose.sh"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

assert_eq() {
  local expected="$1"
  local actual="$2"
  local label="$3"

  [[ "${actual}" == "${expected}" ]] || fail "${label}: expected '${expected}', got '${actual}'"
}

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT
capture="${tmp_dir}/capture"
fake_helper="${tmp_dir}/compose-helper"
env_file="${tmp_dir}/dev.env"
: > "${env_file}"

cat > "${fake_helper}" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
{
  printf 'root=%s\n' "${XG2G_COMPOSE_ROOT:-}"
  printf 'project=%s\n' "${XG2G_COMPOSE_PROJECT:-}"
  printf 'env=%s\n' "${XG2G_ENV_FILE:-}"
  printf 'files=%s\n' "${COMPOSE_FILE:-}"
  printf 'locked=%s\n' "${XG2G_COMPOSE_FILES_LOCKED:-}"
  printf 'args=%s\n' "$*"
} > "${XG2G_TEST_CAPTURE:?}"
EOF
chmod +x "${fake_helper}"

run_case() {
  local runtime="$1"
  local expected_files="$2"

  XG2G_TEST_CAPTURE="${capture}" \
    XG2G_DEV_COMPOSE_HELPER="${fake_helper}" \
    XG2G_DEV_ENV_FILE="${env_file}" \
    "${DEV_COMPOSE}" "${runtime}" ps --all

  assert_eq "root=${REPO_ROOT}/deploy" "$(sed -n '1p' "${capture}")" "${runtime} compose root"
  assert_eq "project=xg2g-dev" "$(sed -n '2p' "${capture}")" "${runtime} project"
  assert_eq "env=${env_file}" "$(sed -n '3p' "${capture}")" "${runtime} env"
  assert_eq "files=${expected_files}" "$(sed -n '4p' "${capture}")" "${runtime} files"
  assert_eq "locked=1" "$(sed -n '5p' "${capture}")" "${runtime} compose lock"
  assert_eq "args=ps --all" "$(sed -n '6p' "${capture}")" "${runtime} arguments"
}

run_case base "docker-compose.yml:../docker-compose.dev.yml"
run_case vaapi "docker-compose.yml:../docker-compose.dev.yml:docker-compose.gpu.yml"
run_case nvidia "docker-compose.yml:../docker-compose.dev.yml:docker-compose.nvidia.yml"

if XG2G_DEV_COMPOSE_HELPER="${fake_helper}" XG2G_DEV_ENV_FILE="${env_file}" \
  "${DEV_COMPOSE}" invalid ps >/dev/null 2>&1; then
  fail "invalid runtime unexpectedly succeeded"
fi

if grep -Eq '^[[:space:]]*@?docker compose' "${REPO_ROOT}/mk/ops.mk" "${REPO_ROOT}/mk/docker.mk"; then
  fail "Make targets must not bypass the canonical development or production lifecycle"
fi

grep -Fq 'Production deployment is not a Make target.' "${REPO_ROOT}/mk/ops.mk" || \
  fail "legacy prod targets must fail closed with production guidance"

echo "OK: start surface contract passed."
