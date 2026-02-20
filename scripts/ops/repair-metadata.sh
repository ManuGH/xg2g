#!/usr/bin/env bash
# Clean macOS metadata artifacts that can break git and worktree hygiene.
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
GIT_COMMON_DIR="$(git rev-parse --git-common-dir)"
BAD_PATTERN='(^|/)\._|(^|/)\.DS_Store$'

cleanup_scope_file_list() {
  local label="$1"
  local list_file="$2"

  local count
  count="$(wc -l < "${list_file}" | tr -d ' ')"
  if [[ "${count}" == "0" ]]; then
    echo "No metadata artifacts found in ${label}"
    return 0
  fi

  local preview=20
  if (( count < preview )); then
    preview="${count}"
  fi

  echo "Removing ${count} metadata file(s) in ${label}"
  head -n "${preview}" "${list_file}"
  if (( count > preview )); then
    echo "... and $((count - preview)) more"
  fi

  while IFS= read -r path; do
    rm -f "${path}"
  done < "${list_file}"
}

cleanup_git_list() {
  local label="$1"
  shift

  local rel_list abs_list
  rel_list="$(mktemp)"
  abs_list="$(mktemp)"
  trap 'rm -f "${rel_list}" "${abs_list}"' RETURN

  git "$@" -z \
    | tr '\0' '\n' \
    | grep -E "${BAD_PATTERN}" > "${rel_list}" || true

  if [[ ! -s "${rel_list}" ]]; then
    echo "No metadata artifacts found in ${label}"
    return 0
  fi

  while IFS= read -r rel; do
    printf "%s/%s\n" "${ROOT}" "${rel}"
  done < "${rel_list}" > "${abs_list}"

  cleanup_scope_file_list "${label}" "${abs_list}"
}

cleanup_git_list "tracked worktree files" ls-files
cleanup_git_list "untracked worktree files" ls-files --others --exclude-standard

git_list="$(mktemp)"
trap 'rm -f "${git_list}"' EXIT
find "${GIT_COMMON_DIR}" -type f \( -name '._*' -o -name '.DS_Store' \) -print > "${git_list}"
cleanup_scope_file_list "git common dir (${GIT_COMMON_DIR})" "${git_list}"

echo "✅ Metadata cleanup complete"
