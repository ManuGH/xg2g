# shellcheck shell=bash
#
# Single authority for HLS/DVR scratch placement validation.
#
# XG2G_HLS_REQUIRE_MOUNT is deliberately host-side: only the host can see the
# mount topology a container is about to be given, and it must be checked
# BEFORE the container is created. This file is that check, and it is the only
# copy. Both the canonical Compose helper and the staging fast-deploy path
# source it so the two cannot drift.
#
# The rule the callers must not re-implement: when a dedicated mount is
# required, DATA and HLS must resolve to different backing mounts. Mount
# identity is authoritative, never path spelling. `/var/lib/xg2g/hls` may or
# may not be a separate filesystem from `/var/lib/xg2g`; only findmnt knows.

xg2g_hls_is_true() {
  case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
    1 | true | yes | on) return 0 ;;
    *) return 1 ;;
  esac
}

xg2g_hls_validate_bool() {
  local label="$1" value="$2"

  case "$(printf '%s' "${value}" | tr '[:upper:]' '[:lower:]')" in
    0 | 1 | false | true | no | yes | off | on | '') return 0 ;;
    *)
      echo "ERROR: ${label} must be a boolean, got: ${value}" >&2
      return 1
      ;;
  esac
}

xg2g_hls_validate_absolute_path() {
  local label="$1" path="$2"

  [[ "${path}" == /* ]] || {
    echo "ERROR: ${label} must be an absolute Linux path: ${path}" >&2
    return 1
  }
  case "${path}" in
    *$'\n'* | *$'\r'*)
      echo "ERROR: ${label} contains a line break" >&2
      return 1
      ;;
  esac
  case "/${path#/}/" in
    *"/../"* | *"/./"* | *"//"*)
      echo "ERROR: ${label} must be a normalized absolute path: ${path}" >&2
      return 1
      ;;
  esac
}

xg2g_hls_path_is_within() {
  local child="${1%/}" parent="${2%/}"

  [[ "${child}" == "${parent}" || "${child}" == "${parent}/"* ]]
}

xg2g_hls_existing_ancestor() {
  local path="$1"

  while [[ ! -e "${path}" && "${path}" != "/" ]]; do
    path="$(dirname "${path}")"
  done
  printf '%s\n' "${path}"
}

# Resolve the backing mount target for a path. A path that does not exist yet
# resolves through its nearest existing ancestor, which is what the kernel
# would do when the directory is created.
xg2g_hls_mount_target() {
  local path
  path="$(xg2g_hls_existing_ancestor "$1")"
  command -v findmnt >/dev/null 2>&1 || return 1
  findmnt -T "${path}" -n -o TARGET 2>/dev/null | head -n 1
}

xg2g_hls_placement() {
  local data_mount hls_mount

  data_mount="$(xg2g_hls_mount_target "$1" 2>/dev/null || true)"
  hls_mount="$(xg2g_hls_mount_target "$2" 2>/dev/null || true)"
  if [[ -n "${data_mount}" && -n "${hls_mount}" && "${data_mount}" != "${hls_mount}" ]]; then
    printf 'dedicated-mount\n'
  else
    printf 'shared-with-data\n'
  fi
}

# Print the resolved storage topology. A misconfigured deployment has to say so
# out loud; silently dropping the dedicated-mount overlay is what let an HLS
# session fill a staging root filesystem.
xg2g_hls_report_topology() {
  local data_root="$1" hls_root="$2" require_mount="$3"

  echo "  XG2G_DATA:              ${data_root}"
  echo "  XG2G_HLS_ROOT:          ${hls_root}"
  echo "  XG2G_HLS_REQUIRE_MOUNT: ${require_mount:-false}"
  echo "  DATA mount:             $(xg2g_hls_mount_target "${data_root}" 2>/dev/null || echo unknown)"
  echo "  HLS mount:              $(xg2g_hls_mount_target "${hls_root}" 2>/dev/null || echo unknown)"
  echo "  placement:              $(xg2g_hls_placement "${data_root}" "${hls_root}")"
}

# The authoritative preflight. Returns non-zero before any container mutation.
#
#   $1 data root, $2 HLS/DVR root, $3 require-mount flag
xg2g_hls_validate_storage() {
  local data_root="$1" hls_root="$2" require_mount="${3:-false}"
  local data_mount hls_mount

  xg2g_hls_validate_absolute_path XG2G_DATA "${data_root}" || return 1
  xg2g_hls_validate_absolute_path XG2G_HLS_ROOT "${hls_root}" || return 1
  [[ "${hls_root}" != "/" ]] || {
    echo "ERROR: XG2G_HLS_ROOT must not be the filesystem root" >&2
    return 1
  }
  xg2g_hls_validate_bool XG2G_HLS_REQUIRE_MOUNT "${require_mount}" || return 1

  # An HLS root outside the data root is operator-provisioned storage: it has
  # to be there and be writable before the container is handed it.
  if ! xg2g_hls_path_is_within "${hls_root}" "${data_root}"; then
    [[ -d "${hls_root}" ]] || {
      echo "ERROR: external XG2G_HLS_ROOT does not exist: ${hls_root}" >&2
      return 1
    }
    [[ -w "${hls_root}" ]] || {
      echo "ERROR: external XG2G_HLS_ROOT is not writable: ${hls_root}" >&2
      return 1
    }
  fi

  # The dedicated-mount requirement is evaluated for every configuration, not
  # only for lexically-external ones. The configuration this has to catch --
  # an HLS root nested inside the data root, on the same filesystem -- is
  # exactly the one a lexical test would skip.
  if xg2g_hls_is_true "${require_mount}"; then
    command -v findmnt >/dev/null 2>&1 || {
      echo "ERROR: XG2G_HLS_REQUIRE_MOUNT=true requires findmnt" >&2
      return 1
    }
    data_mount="$(xg2g_hls_mount_target "${data_root}" 2>/dev/null || true)"
    hls_mount="$(xg2g_hls_mount_target "${hls_root}" 2>/dev/null || true)"
    if [[ -z "${data_mount}" || -z "${hls_mount}" || "${data_mount}" == "${hls_mount}" ]]; then
      echo "ERROR: XG2G_HLS_REQUIRE_MOUNT=true but DVR scratch shares the data mount" >&2
      xg2g_hls_report_topology "${data_root}" "${hls_root}" "${require_mount}" >&2
      return 1
    fi
  fi
}
