#!/usr/bin/env bash
set -euo pipefail

CANONICAL_ROOT="/srv/xg2g"
CANONICAL_ENV_FILE="/etc/xg2g/xg2g.env"
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
  mode="${mode,,}"

  case "${mode}" in
    0|false|no|off)
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

if [[ -f "${ENV_FILE}" ]]; then
  assert_secure_env_file "${ENV_FILE}"
  if compose_file_from_env="$(read_env_value "${ENV_FILE}" COMPOSE_FILE 2>/dev/null)"; then
    COMPOSE_FILE="${compose_file_from_env}"
  fi
fi

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

args=(--project-name "${PROJECT}")
for file in "${compose_files[@]}"; do
  args+=(-f "${file}")
done

if [[ "$#" -gt 0 && "$1" == "config" ]] && config_redaction_enabled; then
  docker compose "${args[@]}" "$@" | redact_compose_output
  exit $?
fi

docker compose "${args[@]}" "$@"
