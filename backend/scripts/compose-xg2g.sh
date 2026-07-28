#!/usr/bin/env bash
set -euo pipefail

CANONICAL_ROOT="/srv/xg2g"
CANONICAL_ENV_FILE="/etc/xg2g/xg2g.env"
DEFAULT_DATA_ROOT="/var/lib/xg2g"
DEFAULT_RECORDINGS_ROOT="/media/nfs-recordings"
DRI_RENDER_GLOB="${XG2G_DRI_RENDER_GLOB:-/dev/dri/renderD*}"
TEMP_FILES=()
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCRIPT_PATH="${SCRIPT_DIR}/$(basename "${BASH_SOURCE[0]}")"

cleanup() {
  local file
  for file in "${TEMP_FILES[@]:-}"; do
    [[ -n "${file}" && -e "${file}" ]] && rm -f "${file}"
  done
  return 0
}
trap cleanup EXIT

trim_ascii_whitespace() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

stat_mode() {
  local path="$1"

  if stat -c '%a' "${path}" >/dev/null 2>&1; then
    stat -c '%a' "${path}"
    return 0
  fi
  stat -f '%Lp' "${path}"
}

stat_owner() {
  local path="$1"

  if stat -c '%u:%g' "${path}" >/dev/null 2>&1; then
    stat -c '%u:%g' "${path}"
    return 0
  fi
  stat -f '%u:%g' "${path}"
}

assert_secure_env_file() {
  local path="$1"
  local mode owner

  [[ "${path}" == "${CANONICAL_ENV_FILE}" ]] || return 0

  mode="$(stat_mode "${path}")"
  [[ "${mode}" == "600" ]] || {
    echo "ERROR: insecure ${path} mode ${mode}; expected 600" >&2
    exit 1
  }

  owner="$(stat_owner "${path}")"
  [[ "${owner}" == "0:0" ]] || {
    echo "ERROR: insecure ${path} owner ${owner}; expected 0:0 (root:root)" >&2
    exit 1
  }
}

read_env_value() {
  local env_file="$1"
  local wanted="$2"
  local raw line key value first_char last_char

  [[ -f "${env_file}" ]] || return 1

  while IFS= read -r raw || [[ -n "${raw}" ]]; do
    line="$(trim_ascii_whitespace "${raw}")"
    [[ -n "${line}" ]] || continue
    [[ "${line:0:1}" == "#" ]] && continue

    if [[ ! "${line}" =~ ^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
      continue
    fi

    key="${BASH_REMATCH[2]}"
    [[ "${key}" == "${wanted}" ]] || continue

    value="$(trim_ascii_whitespace "${BASH_REMATCH[3]}")"
    if [[ ${#value} -ge 2 ]]; then
      first_char="${value:0:1}"
      last_char="${value: -1}"
      if [[ ("${first_char}" == '"' || "${first_char}" == "'") && "${last_char}" == "${first_char}" ]]; then
        printf '%s\n' "${value:1:${#value}-2}"
        return 0
      fi
    fi

    if [[ "${value}" =~ ^#.*$ ]]; then
      printf '\n'
      return 0
    fi
    if [[ "${value}" =~ ^(.*[^[:space:]])[[:space:]]+#.*$ ]]; then
      value="${BASH_REMATCH[1]}"
    fi

    printf '%s\n' "$(trim_ascii_whitespace "${value}")"
    return 0
  done < "${env_file}"

  return 1
}

build_dri_render_overlay() {
  local tmp_file path

  tmp_file="$(mktemp)"
  TEMP_FILES+=("${tmp_file}")

  if compgen -G "${DRI_RENDER_GLOB}" >/dev/null; then
    {
      printf 'services:\n'
      printf '  xg2g:\n'
      printf '    devices:\n'
      for path in ${DRI_RENDER_GLOB}; do
        printf '      - %s:%s\n' "${path}" "${path}"
      done
    } > "${tmp_file}"
  else
    printf 'services:\n  xg2g: {}\n' > "${tmp_file}"
  fi

  printf '%s\n' "${tmp_file}"
}

env_value_or_default() {
  local key="$1"
  local fallback="$2"
  local value=""

  if value="$(read_env_value "${ENV_FILE}" "${key}" 2>/dev/null)"; then
    value="$(trim_ascii_whitespace "${value}")"
  fi
  if [[ -z "${value}" ]]; then
    value="${fallback}"
  fi
  printf '%s\n' "${value}"
}

is_true() {
  case "$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')" in
    1|true|yes|on) return 0 ;;
    *) return 1 ;;
  esac
}

validate_absolute_storage_path() {
  local label="$1"
  local path="$2"

  [[ "${path}" == /* ]] || {
    echo "ERROR: ${label} must be an absolute Linux path: ${path}" >&2
    return 1
  }
  case "${path}" in
    *$'\n'*|*$'\r'*)
      echo "ERROR: ${label} contains a line break" >&2
      return 1
      ;;
  esac
  case "/${path#/}/" in
    *"/../"*|*"/./"*|*"//"*)
      echo "ERROR: ${label} must be a normalized absolute path: ${path}" >&2
      return 1
      ;;
  esac
}

path_is_within() {
  local child="${1%/}"
  local parent="${2%/}"

  [[ "${child}" == "${parent}" || "${child}" == "${parent}/"* ]]
}

existing_storage_ancestor() {
  local path="$1"

  while [[ ! -e "${path}" && "${path}" != "/" ]]; do
    path="$(dirname "${path}")"
  done
  printf '%s\n' "${path}"
}

storage_mount_target() {
  local path
  path="$(existing_storage_ancestor "$1")"
  command -v findmnt >/dev/null 2>&1 || return 1
  findmnt -T "${path}" -n -o TARGET 2>/dev/null | head -n 1
}

storage_medium() {
  local source="$1"
  local fs_type="$2"
  local rotations=""

  case "${fs_type}" in
    tmpfs|ramfs) printf 'ram\n'; return 0 ;;
    nfs|nfs4|cifs|smb3) printf 'network\n'; return 0 ;;
    fuse.mergerfs) printf 'pooled\n'; return 0 ;;
  esac

  if command -v lsblk >/dev/null 2>&1 && [[ "${source}" == /dev/* ]]; then
    rotations="$(lsblk -s -n -o ROTA "${source}" 2>/dev/null | awk '$1 ~ /^[01]$/ {print $1}' | sort -u | tr -d '\n')"
    case "${rotations}" in
      0) printf 'ssd/nvme\n'; return 0 ;;
      1) printf 'hdd\n'; return 0 ;;
      01|10) printf 'mixed\n'; return 0 ;;
    esac
  fi
  printf 'unknown\n'
}

print_storage_row() {
  local role="$1"
  local configured_path="$2"
  local probe_path source fs_type mount_target size available medium

  probe_path="$(existing_storage_ancestor "${configured_path}")"
  source="-"
  fs_type="-"
  mount_target="-"
  size="-"
  available="-"
  medium="unknown"

  if command -v findmnt >/dev/null 2>&1; then
    read -r source fs_type mount_target < <(findmnt -T "${probe_path}" -n -o SOURCE,FSTYPE,TARGET 2>/dev/null | head -n 1) || true
  fi
  if command -v df >/dev/null 2>&1; then
    read -r size available < <(df -hP "${probe_path}" 2>/dev/null | awk 'NR==2 {print $2, $4}') || true
  fi
  medium="$(storage_medium "${source}" "${fs_type}")"

  printf '%-12s %-34s %-18s %-12s %-10s %-9s %-9s\n' \
    "${role}" "${configured_path}" "${mount_target}" "${fs_type}" "${medium}" "${size}" "${available}"
}

print_storage_layout() {
  local data_root hls_root recordings_root data_mount hls_mount placement

  data_root="$(env_value_or_default XG2G_DATA "${DEFAULT_DATA_ROOT}")"
  hls_root="$(env_value_or_default XG2G_HLS_ROOT "${data_root%/}/hls")"
  recordings_root="${DEFAULT_RECORDINGS_ROOT}"

  validate_absolute_storage_path XG2G_DATA "${data_root}"
  validate_absolute_storage_path XG2G_HLS_ROOT "${hls_root}"
  [[ "${hls_root}" != "/" ]] || {
    echo "ERROR: XG2G_HLS_ROOT must not be the filesystem root" >&2
    return 1
  }
  validate_absolute_storage_path recordings-root "${recordings_root}"

  printf '%-12s %-34s %-18s %-12s %-10s %-9s %-9s\n' \
    "ROLE" "PATH" "MOUNT" "FILESYSTEM" "MEDIUM" "SIZE" "AVAILABLE"
  print_storage_row "persistent" "${data_root}"
  print_storage_row "dvr-scratch" "${hls_root}"
  print_storage_row "recordings" "${recordings_root}"

  data_mount="$(storage_mount_target "${data_root}" 2>/dev/null || true)"
  hls_mount="$(storage_mount_target "${hls_root}" 2>/dev/null || true)"
  placement="shared-with-data"
  if [[ -n "${data_mount}" && -n "${hls_mount}" && "${data_mount}" != "${hls_mount}" ]]; then
    placement="dedicated-mount"
  fi
  printf 'DVR_PLACEMENT=%s\n' "${placement}"
  printf 'DVR_MOUNT_REQUIRED=%s\n' "$(env_value_or_default XG2G_HLS_REQUIRE_MOUNT false)"
}

validate_hls_storage() {
  local data_root hls_root require_mount data_mount hls_mount

  data_root="$(env_value_or_default XG2G_DATA "${DEFAULT_DATA_ROOT}")"
  hls_root="$(env_value_or_default XG2G_HLS_ROOT "${data_root%/}/hls")"
  require_mount="$(env_value_or_default XG2G_HLS_REQUIRE_MOUNT false)"

  validate_absolute_storage_path XG2G_DATA "${data_root}"
  validate_absolute_storage_path XG2G_HLS_ROOT "${hls_root}"
  [[ "${hls_root}" != "/" ]] || {
    echo "ERROR: XG2G_HLS_ROOT must not be the filesystem root" >&2
    return 1
  }
  case "$(printf '%s' "${require_mount}" | tr '[:upper:]' '[:lower:]')" in
    0|1|false|true|no|yes|off|on) ;;
    *)
      echo "ERROR: XG2G_HLS_REQUIRE_MOUNT must be a boolean, got: ${require_mount}" >&2
      return 1
      ;;
  esac

  if ! path_is_within "${hls_root}" "${data_root}"; then
    [[ -d "${hls_root}" ]] || {
      echo "ERROR: external XG2G_HLS_ROOT does not exist: ${hls_root}" >&2
      return 1
    }
    [[ -w "${hls_root}" ]] || {
      echo "ERROR: external XG2G_HLS_ROOT is not writable: ${hls_root}" >&2
      return 1
    }
  fi

  if is_true "${require_mount}"; then
    command -v findmnt >/dev/null 2>&1 || {
      echo "ERROR: XG2G_HLS_REQUIRE_MOUNT=true requires findmnt" >&2
      return 1
    }
    data_mount="$(storage_mount_target "${data_root}")"
    hls_mount="$(storage_mount_target "${hls_root}")"
    [[ -n "${data_mount}" && -n "${hls_mount}" && "${data_mount}" != "${hls_mount}" ]] || {
      echo "ERROR: XG2G_HLS_REQUIRE_MOUNT=true but DVR scratch shares the data mount (${data_mount:-unknown})" >&2
      return 1
    }
  fi
}

build_hls_storage_overlay() {
  local data_root hls_root tmp_file quoted_path

  data_root="$(env_value_or_default XG2G_DATA "${DEFAULT_DATA_ROOT}")"
  hls_root="$(env_value_or_default XG2G_HLS_ROOT "${data_root%/}/hls")"
  validate_hls_storage

  if path_is_within "${hls_root}" "${data_root}"; then
    return 1
  fi

  tmp_file="$(mktemp)"
  TEMP_FILES+=("${tmp_file}")
  quoted_path="${hls_root//\'/\'\'}"
  cat > "${tmp_file}" <<EOF
services:
  xg2g:
    volumes:
      - type: bind
        source: '${quoted_path}'
        target: '${quoted_path}'
EOF
  printf '%s\n' "${tmp_file}"
}

if [[ "${SCRIPT_PATH}" == "${CANONICAL_ROOT}/scripts/compose-xg2g.sh" && -f "${CANONICAL_ENV_FILE}" ]]; then
  assert_secure_env_file "${CANONICAL_ENV_FILE}"
fi

ROOT="${XG2G_COMPOSE_ROOT:-/srv/xg2g}"
PROJECT="${XG2G_COMPOSE_PROJECT:-xg2g}"
ENV_FILE="${XG2G_ENV_FILE:-/etc/xg2g/xg2g.env}"
BASE_FILE="${ROOT}/docker-compose.yml"
GPU_FILE="${ROOT}/docker-compose.gpu.yml"
NVIDIA_FILE="${ROOT}/docker-compose.nvidia.yml"

resolve_compose_file() {
  local file="$1"

  if [[ -z "${file}" ]]; then
    echo "ERROR: empty compose file entry in COMPOSE_FILE" >&2
    exit 1
  fi

  if [[ "${file}" = /* ]]; then
    printf '%s\n' "${file}"
    return 0
  fi

  printf '%s/%s\n' "${ROOT}" "${file}"
}

config_redaction_enabled() {
  local mode="${XG2G_COMPOSE_CONFIG_REDACT:-1}"

  case "${mode}" in
    0|[Ff][Aa][Ll][Ss][Ee]|[Nn][Oo]|[Oo][Ff][Ff])
      return 1
      ;;
  esac

  return 0
}

redact_compose_output() {
  awk '
function ltrim(value) {
  sub(/^[[:space:]]+/, "", value)
  return value
}
function is_secret_key(key) {
  key = tolower(key)
  return key ~ /(^|[._-])(token|secret|password|passwd|pass|api[_-]?key)([._-]|$)/
}
function redact_url(value, redacted) {
  redacted = value
  gsub(/:\/\/[^\/[:space:]\"]+:[^@\/[:space:]\"]+@/, "://REDACTED@", redacted)
  return redacted
}
{
  line = $0

  stripped = line
  sub(/^[[:space:]]*-[[:space:]]*/, "", stripped)
  eq = index(stripped, "=")
  if (eq > 1) {
    key = substr(stripped, 1, eq - 1)
    value = substr(stripped, eq + 1)
    prefix = substr(line, 1, length(line) - length(stripped))
    if (is_secret_key(key)) {
      print prefix key "=REDACTED"
      next
    }
    redacted = redact_url(value)
    if (redacted != value) {
      print prefix key "=" redacted
      next
    }
  }

  stripped = line
  sub(/^[[:space:]]*/, "", stripped)
  colon = index(stripped, ":")
  if (colon > 1) {
    key = substr(stripped, 1, colon - 1)
    value = ltrim(substr(stripped, colon + 1))
    prefix = substr(line, 1, length(line) - length(stripped))
    if (is_secret_key(key)) {
      print prefix key ": REDACTED"
      next
    }
    redacted = redact_url(value)
    if (redacted != value) {
      print prefix key ": " redacted
      next
    }
  }

  print line
}
'
}

compose_files_locked="${XG2G_COMPOSE_FILES_LOCKED:-0}"

if [[ -f "${ENV_FILE}" ]]; then
  assert_secure_env_file "${ENV_FILE}"
  case "${compose_files_locked}" in
    1|[Tt][Rr][Uu][Ee]|[Yy][Ee][Ss]|[Oo][Nn])
      ;;
    *)
      if compose_file_from_env="$(read_env_value "${ENV_FILE}" COMPOSE_FILE 2>/dev/null)"; then
        COMPOSE_FILE="${compose_file_from_env}"
      fi
      ;;
  esac
fi

case "${1:-}" in
  --storage-layout)
    [[ "$#" -eq 1 ]] || {
      echo "ERROR: --storage-layout accepts no additional arguments" >&2
      exit 1
    }
    print_storage_layout
    exit 0
    ;;
  --storage-check)
    [[ "$#" -eq 1 ]] || {
      echo "ERROR: --storage-check accepts no additional arguments" >&2
      exit 1
    }
    validate_hls_storage
    echo "OK: HLS/DVR storage contract holds."
    exit 0
    ;;
esac

compose_files=()
if [[ -n "${COMPOSE_FILE:-}" ]]; then
  raw_compose_files=()
  IFS=':' read -r -a raw_compose_files <<< "${COMPOSE_FILE}"
  for file in "${raw_compose_files[@]}"; do
    compose_files+=("$(resolve_compose_file "${file}")")
  done
else
  compose_files=("${BASE_FILE}")
  for file in "${GPU_FILE}" "${NVIDIA_FILE}"; do
    if [[ -f "${file}" ]]; then
      compose_files+=("${file}")
    fi
  done
fi

if [[ "${#compose_files[@]}" -eq 0 ]]; then
  echo "ERROR: no compose files resolved" >&2
  exit 1
fi

for file in "${compose_files[@]}"; do
  if [[ ! -f "${file}" ]]; then
    echo "ERROR: compose file not found: ${file}" >&2
    exit 1
  fi
done

if [[ "$#" -gt 0 && "$1" == "--print-files" ]]; then
  for file in "${compose_files[@]}"; do
    printf '%s\n' "${file}"
  done
  exit 0
fi

for file in "${compose_files[@]}"; do
  if [[ "${file}" == "${GPU_FILE}" ]]; then
    # Materialize only visible render nodes instead of binding the whole /dev/dri tree.
    compose_files+=("$(build_dri_render_overlay)")
    break
  fi
done

if hls_storage_overlay="$(build_hls_storage_overlay)"; then
  compose_files+=("${hls_storage_overlay}")
fi

args=(--project-name "${PROJECT}")
for file in "${compose_files[@]}"; do
  args+=(-f "${file}")
done

if [[ "$#" -gt 0 && "$1" == "config" ]] && config_redaction_enabled; then
  docker compose "${args[@]}" "$@" | redact_compose_output
  exit $?
fi

docker compose "${args[@]}" "$@"
