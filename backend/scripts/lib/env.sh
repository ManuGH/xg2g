#!/usr/bin/env bash

trim_ascii_whitespace() {
  local value="$1"

  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "${value}"
}

PARSED_ENV_KEY=""
PARSED_ENV_VALUE=""

parse_env_assignment() {
  local raw="$1"
  local env_file="$2"
  local line key value first_char last_char

  PARSED_ENV_KEY=""
  PARSED_ENV_VALUE=""

  line="$(trim_ascii_whitespace "${raw}")"
  [[ -n "${line}" ]] || return 1
  [[ "${line:0:1}" == "#" ]] && return 1

  if [[ ! "${line}" =~ ^(export[[:space:]]+)?([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=(.*)$ ]]; then
    echo "ERROR: unsupported env line in ${env_file}: ${raw}" >&2
    return 2
  fi

  key="${BASH_REMATCH[2]}"
  value="$(trim_ascii_whitespace "${BASH_REMATCH[3]}")"
  if [[ ${#value} -ge 2 ]]; then
    first_char="${value:0:1}"
    last_char="${value: -1}"
    if [[ ("${first_char}" == '"' || "${first_char}" == "'") && "${last_char}" == "${first_char}" ]]; then
      value="${value:1:${#value}-2}"
    fi
  fi

  if [[ "${value}" =~ ^#.*$ ]]; then
    value=""
  elif [[ "${value}" =~ ^(.*[^[:space:]])[[:space:]]+#.*$ ]]; then
    value="${BASH_REMATCH[1]}"
  fi

  PARSED_ENV_KEY="${key}"
  PARSED_ENV_VALUE="$(trim_ascii_whitespace "${value}")"
  return 0
}

read_env_value() {
  local env_file="$1"
  local wanted="$2"
  local raw status

  [[ -f "${env_file}" ]] || return 1

  while IFS= read -r raw || [[ -n "${raw}" ]]; do
    parse_env_assignment "${raw}" "${env_file}" || {
      status=$?
      [[ "${status}" -eq 1 ]] && continue
      return "${status}"
    }

    [[ "${PARSED_ENV_KEY}" == "${wanted}" ]] || continue
    printf '%s\n' "${PARSED_ENV_VALUE}"
    return 0
  done < "${env_file}"

  return 1
}

export_env_file_safely() {
  local env_file="$1"
  local raw status

  [[ -f "${env_file}" ]] || {
    echo "ERROR: missing env file: ${env_file}" >&2
    return 1
  }

  while IFS= read -r raw || [[ -n "${raw}" ]]; do
    parse_env_assignment "${raw}" "${env_file}" || {
      status=$?
      [[ "${status}" -eq 1 ]] && continue
      return "${status}"
    }

    printf -v "${PARSED_ENV_KEY}" '%s' "${PARSED_ENV_VALUE}"
    export "${PARSED_ENV_KEY}"
  done < "${env_file}"
}

resolve_selected_go_bin() {
  local go_bin

  if ! command -v go >/dev/null 2>&1; then
    echo "ERROR: missing required command: go" >&2
    return 1
  fi

  go_bin="$(go env GOROOT)/bin/go" || return 1
  if [[ ! -x "${go_bin}" ]]; then
    echo "ERROR: selected Go toolchain binary not found: ${go_bin}" >&2
    return 1
  fi

  printf '%s\n' "${go_bin}"
}
