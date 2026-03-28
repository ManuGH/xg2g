#!/usr/bin/env bash
set -euo pipefail

ROOT="${XG2G_COMPOSE_ROOT:-/srv/xg2g}"
PROJECT="${XG2G_COMPOSE_PROJECT:-xg2g}"
ENV_FILE="${XG2G_ENV_FILE:-/etc/xg2g/xg2g.env}"
BASE_FILE="${ROOT}/docker-compose.yml"
GPU_FILE="${ROOT}/docker-compose.gpu.yml"

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
  set -a
  # shellcheck disable=SC1090
  . "${ENV_FILE}"
  set +a
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
  if [[ -f "${GPU_FILE}" ]]; then
    compose_files+=("${GPU_FILE}")
  fi
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

args=(--project-name "${PROJECT}")
for file in "${compose_files[@]}"; do
  args+=(-f "${file}")
done

if [[ "$#" -gt 0 && "$1" == "config" ]] && config_redaction_enabled; then
  docker compose "${args[@]}" "$@" | redact_compose_output
  exit $?
fi

exec docker compose "${args[@]}" "$@"
