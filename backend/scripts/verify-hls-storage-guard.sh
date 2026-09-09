#!/usr/bin/env bash
#
# Regression gate for the shared HLS/DVR placement preflight.
#
# Two defects this exists to prevent:
#
#  1. XG2G_HLS_REQUIRE_MOUNT=true was evaluated only when XG2G_HLS_ROOT was
#     lexically outside XG2G_DATA, so the dangerous configuration -- HLS nested
#     inside the data root on the same filesystem -- skipped the check and
#     staging wrote a 12 GiB DVR session onto its root filesystem.
#
#  2. The check then compared mountpoints. A bind mount has a mountpoint of its
#     own and none of its own capacity, so a TARGET comparison reports
#     "dedicated" for storage that still fills the same filesystem.
#
# Mount topology and device identity are both simulated, with findmnt and stat
# stubs, so the gate is hermetic and needs no privileges.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIB="${SCRIPT_DIR}/lib/hls-storage.sh"
[[ -r "${LIB}" ]] || { echo "❌ shared storage library missing: ${LIB}" >&2; exit 1; }

WORK="$(mktemp -d)"
trap 'rm -rf "${WORK}"' EXIT

FAILURES=0
CASES=0

mkdir -p "${WORK}/bin"

# --- findmnt stub: path -> mountpoint (longest prefix wins) ------------------
cat > "${WORK}/bin/findmnt" <<'STUB'
#!/usr/bin/env bash
target=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -T) shift; target="$1" ;;
  esac
  shift
done
best=""; answer="/"
while IFS='=' read -r prefix value; do
  [[ -n "${prefix}" ]] || continue
  if [[ "${target}" == "${prefix}" || "${target}" == "${prefix}/"* ]]; then
    if [[ ${#prefix} -ge ${#best} ]]; then best="${prefix}"; answer="${value}"; fi
  fi
done < "${MOUNT_TABLE}"
printf '%s\n' "${answer}"
STUB

# --- stat stub: path -> st_dev (longest prefix wins) -------------------------
# A bind mount and its source share an st_dev; that is the whole point.
cat > "${WORK}/bin/stat" <<'STUB'
#!/usr/bin/env bash
target=""
for arg in "$@"; do
  case "${arg}" in
    -c|-f|'%d'|--) ;;
    *) target="${arg}" ;;
  esac
done
best=""; answer="2049"
while IFS='=' read -r prefix value; do
  [[ -n "${prefix}" ]] || continue
  if [[ "${target}" == "${prefix}" || "${target}" == "${prefix}/"* ]]; then
    if [[ ${#prefix} -ge ${#best} ]]; then best="${prefix}"; answer="${value}"; fi
  fi
done < "${DEVICE_TABLE}"
printf '%s\n' "${answer}"
STUB

chmod +x "${WORK}/bin/findmnt" "${WORK}/bin/stat"
export PATH="${WORK}/bin:${PATH}"
export MOUNT_TABLE="${WORK}/mounts"
export DEVICE_TABLE="${WORK}/devices"

set_topology() {
  # set_topology <mounts-csv> -- <devices-csv>
  : > "${WORK}/mounts"; : > "${WORK}/devices"
  local into="mounts" entry
  for entry in "$@"; do
    if [[ "${entry}" == "--" ]]; then into="devices"; continue; fi
    printf '%s\n' "${entry}" >> "${WORK}/${into}"
  done
}

# shellcheck source=lib/hls-storage.sh
source "${LIB}"

expect() {
  local want="$1" name="$2" data="$3" hls="$4" req="$5"
  CASES=$((CASES + 1))
  local rc=0
  xg2g_hls_validate_storage "${data}" "${hls}" "${req}" >/dev/null 2>&1 || rc=$?
  if { [[ "${want}" == "pass" ]] && [[ "${rc}" -eq 0 ]]; } || { [[ "${want}" == "fail" ]] && [[ "${rc}" -ne 0 ]]; }; then
    echo "  ✅ ${name}"
  else
    echo "  ❌ ${name} (wanted ${want}, rc=${rc})"
    FAILURES=$((FAILURES + 1))
  fi
}

mkdir -p "${WORK}/data/hls" "${WORK}/scratch" "${WORK}/bindtarget"

echo "== dedicated-filesystem preflight =="

# 1. Same filesystem, same mountpoint. The original defect.
set_topology "/=/" -- "/=2049"
expect fail "1. nested HLS root on the same filesystem with REQUIRE=true is refused" \
  "${WORK}/data" "${WORK}/data/hls" true

# 2. THE BIND-MOUNT HOLE. Distinct mountpoints, one filesystem: a TARGET
#    comparison calls this dedicated, and the root filesystem still fills up.
set_topology "/=/" "${WORK}/bindtarget=${WORK}/bindtarget" \
          -- "/=2049"
expect fail "2. distinct mountpoints backed by the SAME filesystem are refused (bind mount)" \
  "${WORK}/data" "${WORK}/bindtarget" true

# 3. External path on genuinely separate storage.
set_topology "/=/" "${WORK}/scratch=${WORK}/scratch" \
          -- "/=2049" "${WORK}/scratch=2065"
expect pass "3. external HLS root on a different filesystem with REQUIRE=true is allowed" \
  "${WORK}/data" "${WORK}/scratch" true

# 4. Nested path that really is its own filesystem: identity decides, not spelling.
set_topology "/=/" "${WORK}/data/hls=${WORK}/data/hls" \
          -- "/=2049" "${WORK}/data/hls=2066"
expect pass "4. nested HLS root that IS its own filesystem with REQUIRE=true is allowed" \
  "${WORK}/data" "${WORK}/data/hls" true

# 5/6. Portable profile: sharing is legal when no dedicated storage is demanded.
set_topology "/=/" -- "/=2049"
expect pass "5. shared filesystem with REQUIRE=false remains allowed (portable profile)" \
  "${WORK}/data" "${WORK}/data/hls" false
expect pass "6. shared filesystem with REQUIRE unset remains allowed" \
  "${WORK}/data" "${WORK}/data/hls" ""

# 7. External storage must exist before a container is handed it.
expect fail "7. missing external HLS root is refused" \
  "${WORK}/data" "${WORK}/does-not-exist" false

# 8. ...and be writable.
if [[ "${EUID}" -ne 0 ]]; then
  mkdir -p "${WORK}/readonly"
  chmod 500 "${WORK}/readonly"
  expect fail "8. non-writable external HLS root is refused" \
    "${WORK}/data" "${WORK}/readonly" false
  chmod 700 "${WORK}/readonly"
else
  echo "  ⏭  8. non-writable external HLS root (skipped: running as root)"
fi

# 9. A not-yet-created HLS directory resolves through its existing ancestor and
#    must be classified as that ancestor's filesystem, not an imagined device.
set_topology "/=/" -- "/=2049"
expect fail "9. not-yet-created HLS dir under the data filesystem is refused, not assumed dedicated" \
  "${WORK}/data" "${WORK}/data/hls/not/created/yet" true

# 10. Fail closed when identity cannot be established at all.
CASES=$((CASES + 1))
identity_rc=0
( PATH="/nonexistent"; xg2g_hls_validate_storage "${WORK}/data" "${WORK}/scratch" true ) >/dev/null 2>&1 || identity_rc=$?
if [[ "${identity_rc}" -ne 0 ]]; then
  echo "  ✅ 10. unverifiable filesystem identity is refused, not treated as different"
else
  echo "  ❌ 10. unverifiable filesystem identity was accepted"
  FAILURES=$((FAILURES + 1))
fi

# Input hygiene.
set_topology "/=/" -- "/=2049"
expect fail "11. non-boolean REQUIRE value is refused" "${WORK}/data" "${WORK}/data/hls" maybe
expect fail "12. filesystem root as HLS root is refused" "${WORK}/data" "/" false
expect fail "13. relative HLS root is refused" "${WORK}/data" "relative/path" false
expect fail "14. non-normalized HLS root is refused" "${WORK}/data" "${WORK}/data/../data/hls" false

# --- both callers must use the shared authority ----------------------------
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

# The safety decision must not be a mountpoint comparison.
CASES=$((CASES + 1))
guard_lib="${REPO_ROOT}/backend/scripts/lib/hls-storage.sh"
if grep -qE '\$\{data_fs\}"?[[:space:]]*==[[:space:]]*"?\$\{hls_fs\}' "${guard_lib}" &&
   ! grep -qE '\$\{data_mount\}"?[[:space:]]*(==|!=)[[:space:]]*"?\$\{hls_mount\}' "${guard_lib}"; then
  echo "  ✅ shared preflight decides on filesystem identity, not mountpoints"
else
  echo "  ❌ shared preflight must compare filesystem identity, never mountpoints"
  FAILURES=$((FAILURES + 1))
fi

echo
if [[ "${FAILURES}" -eq 0 ]]; then
  echo "✅ HLS storage guard: ${CASES} checks passed"
else
  echo "❌ HLS storage guard: ${FAILURES} of ${CASES} checks failed"
  exit 1
fi
