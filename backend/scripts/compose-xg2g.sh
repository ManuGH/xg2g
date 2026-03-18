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

exec docker compose "${args[@]}" "$@"
