#!/usr/bin/env bash
#
# Regression gate for the shared HLS/DVR placement preflight.
#
# The defect this exists to prevent: XG2G_HLS_REQUIRE_MOUNT=true was evaluated
# only when XG2G_HLS_ROOT was lexically outside XG2G_DATA. The dangerous
# configuration -- HLS nested inside the data root, on the same filesystem --
# skipped the check entirely, and staging wrote a 12 GiB DVR session onto its
# root filesystem until the disk filled.
#
# Mount topology is simulated with a findmnt stub so the gate is hermetic and
# needs no privileges.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${SCRIPT_DIR}/lib/hls-storage.sh"
[[ -r "${LIB}" ]] || { echo "❌ shared storage library missing: ${LIB}" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

FAILURES=0
CASES=0

# --- findmnt stub -----------------------------------------------------------
# MOUNT_TABLE maps a path prefix to a mount target, longest prefix wins.
mkdir -p "${WORK}/bin"
cat > "${WORK}/bin/findmnt" <<'STUB'
#!/usr/bin/env bash
# Minimal stand-in for `findmnt -T <path> -n -o TARGET`.
target=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -T) shift; target="$1" ;;
    *) ;;
  esac
  shift
done
best=""
while IFS='=' read -r prefix mount; do
  [[ -n "${prefix}" ]] || continue
  if [[ "${target}" == "${prefix}" || "${target}" == "${prefix}/"* ]]; then
    if [[ ${#prefix} -gt ${#best} ]]; then best="${prefix}"; echo "${mount}" > "${TMPDIR_STUB}/answer"; fi
  fi
done < "${MOUNT_TABLE}"
if [[ -n "${best}" ]]; then cat "${TMPDIR_STUB}/answer"; else echo "/"; fi
STUB
chmod +x "${WORK}/bin/findmnt"
export TMPDIR_STUB="${WORK}"
export PATH="${WORK}/bin:${PATH}"

set_mounts() {
  : > "${WORK}/mounts"
  local entry
  for entry in "$@"; do printf '%s\n' "${entry}" >> "${WORK}/mounts"; done
  export MOUNT_TABLE="${WORK}/mounts"
}

# shellcheck source=lib/hls-storage.sh
source "${LIB}"

expect() {
  local want="$1" name="$2" data="$3" hls="$4" req="$5"
  CASES=$((CASES + 1))
  local rc=0
  xg2g_hls_validate_storage "${data}" "${hls}" "${req}" >/dev/null 2>&1 || rc=$?
  if [[ "${want}" == "pass" && "${rc}" -eq 0 ]] || [[ "${want}" == "fail" && "${rc}" -ne 0 ]]; then
    echo "  ✅ ${name}"
  else
    echo "  ❌ ${name} (wanted ${want}, rc=${rc})"
    FAILURES=$((FAILURES + 1))
  fi
}

echo "== HLS/DVR dedicated-mount preflight =="

# The regression for the actual defect: nested path, same filesystem, required.
mkdir -p "${WORK}/data/hls"
set_mounts "/=/"
expect fail "nested HLS root on the SAME mount with REQUIRE=true is refused" \
  "${WORK}/data" "${WORK}/data/hls" true

# Mount identity, not path spelling: a nested path that really is its own
# filesystem is a legitimate dedicated mount and must be allowed.
set_mounts "/=/" "${WORK}/data/hls=${WORK}/data/hls"
expect pass "nested HLS root that IS its own mount with REQUIRE=true is allowed" \
  "${WORK}/data" "${WORK}/data/hls" true

# External path, distinct mount: the configuration that was already working.
mkdir -p "${WORK}/scratch"
set_mounts "/=/" "${WORK}/scratch=${WORK}/scratch"
expect pass "external HLS root on a distinct mount with REQUIRE=true is allowed" \
  "${WORK}/data" "${WORK}/scratch" true

# External path that shares the data mount must still be refused.
set_mounts "/=/"
expect fail "external HLS root sharing the data mount with REQUIRE=true is refused" \
  "${WORK}/data" "${WORK}/scratch" true

# Portable profile: no dedicated mount demanded, shared placement stays legal.
set_mounts "/=/"
expect pass "shared mount with REQUIRE=false remains allowed (portable profile)" \
  "${WORK}/data" "${WORK}/data/hls" false
expect pass "shared mount with REQUIRE unset remains allowed" \
  "${WORK}/data" "${WORK}/data/hls" ""

# External storage has to exist and be usable before a container gets it.
set_mounts "/=/" "${WORK}/scratch=${WORK}/scratch"
expect fail "missing external HLS root is refused" \
  "${WORK}/data" "${WORK}/does-not-exist" false

if [[ "${EUID}" -ne 0 ]]; then
  mkdir -p "${WORK}/readonly"
  chmod 500 "${WORK}/readonly"
  expect fail "non-writable external HLS root is refused" \
    "${WORK}/data" "${WORK}/readonly" false
  chmod 700 "${WORK}/readonly"
else
  echo "  ⏭  non-writable external HLS root (skipped: running as root)"
fi

# Input hygiene.
set_mounts "/=/"
expect fail "non-boolean REQUIRE value is refused" \
  "${WORK}/data" "${WORK}/data/hls" maybe
expect fail "filesystem root as HLS root is refused" \
  "${WORK}/data" "/" false
expect fail "relative HLS root is refused" \
  "${WORK}/data" "relative/path" false
expect fail "non-normalized HLS root is refused" \
  "${WORK}/data" "${WORK}/data/../data/hls" false

# --- both callers must use the shared authority, not their own copy ---------
echo
echo "== single-authority wiring =="
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
check_sources() {
  local file="$1" label="$2"
  CASES=$((CASES + 1))
  if grep -q 'hls-storage.sh' "${file}"; then
    echo "  ✅ ${label} uses the shared preflight"
  else
    echo "  ❌ ${label} does not reference lib/hls-storage.sh"
    FAILURES=$((FAILURES + 1))
  fi
}
check_sources "${REPO_ROOT}/backend/scripts/compose-xg2g.sh" "compose helper"
check_sources "${REPO_ROOT}/scripts/deploy-staging-fast.sh" "staging fast-deploy"

CASES=$((CASES + 1))
if grep -qE 'hls_root.*!=[[:space:]]*/var/lib/xg2g/\*' "${REPO_ROOT}/scripts/deploy-staging-fast.sh"; then
  echo "  ❌ staging fast-deploy still gates storage validation behind a lexical path test"
  FAILURES=$((FAILURES + 1))
else
  echo "  ✅ staging fast-deploy no longer gates validation on path spelling"
fi

echo
if [[ "${FAILURES}" -eq 0 ]]; then
  echo "✅ HLS storage guard: ${CASES} checks passed"
else
  echo "❌ HLS storage guard: ${FAILURES} of ${CASES} checks failed"
  exit 1
fi
