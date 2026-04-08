#!/usr/bin/env bash
set -euo pipefail

event_name="${1:-}"
base_sha="${2:-}"
head_sha="${3:-HEAD}"
before_sha="${4:-}"
head_ref="${5:-HEAD}"

if [[ -z "$event_name" ]]; then
  echo "usage: $0 <event_name> [base_sha] [head_sha] [before_sha] [head_ref]" >&2
  exit 2
fi

emit_outputs() {
  local env_access_needed="$1"
  local deprecations_needed="$2"
  local webui_lint_needed="$3"
  local forced_full_scope="$4"
  local reason="$5"

  cat <<EOF
env_access_needed=${env_access_needed}
deprecations_needed=${deprecations_needed}
webui_lint_needed=${webui_lint_needed}
forced_full_scope=${forced_full_scope}
reason=${reason}
EOF
}

emit_all_true() {
  emit_outputs "true" "true" "true" "true" "$1"
}

resolve_changed_files() {
  local changed_files=""

  if [[ -n "${XG2G_CI_CHANGED_FILES_PATH:-}" ]]; then
    if [[ ! -f "${XG2G_CI_CHANGED_FILES_PATH}" ]]; then
      force_reason="changed_files_override_missing"
      return 1
    fi
    cat "${XG2G_CI_CHANGED_FILES_PATH}"
    return 0
  fi

  case "$event_name" in
    workflow_dispatch)
      force_reason="workflow_dispatch_full_run"
      return 1
      ;;
    pull_request)
      if [[ -z "$base_sha" || -z "$head_sha" ]]; then
        force_reason="missing_pr_shas"
        return 1
      fi
      if ! git cat-file -e "${base_sha}^{commit}" 2>/dev/null; then
        force_reason="missing_pr_base_commit"
        return 1
      fi
      if ! git cat-file -e "${head_sha}^{commit}" 2>/dev/null; then
        force_reason="missing_pr_head_commit"
        return 1
      fi
      if ! git merge-base "$base_sha" "$head_sha" >/dev/null 2>&1; then
        force_reason="missing_pr_merge_base"
        return 1
      fi
      if ! changed_files="$(git diff --name-only "${base_sha}...${head_sha}" 2>/dev/null)"; then
        force_reason="pr_diff_failed"
        return 1
      fi
      printf '%s\n' "$changed_files"
      return 0
      ;;
    push)
      if [[ -n "$before_sha" && "$before_sha" != "0000000000000000000000000000000000000000" ]]; then
        if ! git cat-file -e "${before_sha}^{commit}" 2>/dev/null; then
          force_reason="missing_push_before_commit"
          return 1
        fi
        if ! git cat-file -e "${head_sha}^{commit}" 2>/dev/null; then
          force_reason="missing_push_head_commit"
          return 1
        fi
        if ! changed_files="$(git diff --name-only "${before_sha}..${head_sha}" 2>/dev/null)"; then
          force_reason="push_diff_failed"
          return 1
        fi
        printf '%s\n' "$changed_files"
        return 0
      fi

      if ! changed_files="$(git show --pretty='' --name-only "$head_ref" 2>/dev/null)"; then
        force_reason="push_show_failed"
        return 1
      fi
      printf '%s\n' "$changed_files"
      return 0
      ;;
    *)
      force_reason="unknown_event_${event_name}"
      return 1
      ;;
  esac
}

force_reason=""
tmp_output="$(mktemp)"
trap 'rm -f "${tmp_output}"' EXIT

if ! resolve_changed_files >"${tmp_output}"; then
  emit_all_true "${force_reason:-scope_resolution_failed}"
  exit 0
fi

changed_files="$(cat "${tmp_output}")"

if printf '%s\n' "$changed_files" | grep -Eq '^(backend/scripts/ci/resolve-lint-scope\.sh|\.github/workflows/lint\.yml)$'; then
  emit_all_true "scope_logic_changed"
  exit 0
fi

env_access_needed=false
if printf '%s\n' "$changed_files" | grep -Eq '^(backend/internal/|backend/cmd/).+\.go$'; then
  env_access_needed=true
fi

deprecations_needed=false
if printf '%s\n' "$changed_files" | grep -Eq '^(docs/deprecations\.json|docs/DEPRECATION_POLICY\.md|backend/internal/config/deprecation\.go|backend/internal/config/registry\.go|backend/internal/auth/token\.go|backend/scripts/check_deprecations\.py)$'; then
  deprecations_needed=true
fi

webui_lint_needed=false
if printf '%s\n' "$changed_files" | grep -Eq '^frontend/webui/'; then
  webui_lint_needed=true
fi

emit_outputs "$env_access_needed" "$deprecations_needed" "$webui_lint_needed" "false" "scoped_diff"
