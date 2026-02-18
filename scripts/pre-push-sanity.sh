#!/usr/bin/env bash
# Fail-closed pre-push guard for shared SMB/macOS worktrees.
set -euo pipefail

MAX_UNTRACKED="${XG2G_MAX_UNTRACKED:-200}"
BAD_PATTERN='(^|/)\._|(^|/)\.DS_Store$|(^|/)\.Spotlight-V100(/|$)|(^|/)\.Trashes(/|$)'

fail() {
  echo "pre-push sanity check failed:"
  echo "  - $1"
  echo ""
  echo "If this is intentional, bypass once with: git push --no-verify"
  exit 1
}

if ! [[ "${MAX_UNTRACKED}" =~ ^[0-9]+$ ]]; then
  fail "XG2G_MAX_UNTRACKED must be a non-negative integer (got '${MAX_UNTRACKED}')"
fi

current_branch="$(git symbolic-ref --quiet --short HEAD || true)"
if [[ -z "${current_branch}" ]]; then
  fail "detached HEAD is not allowed for normal pushes"
fi

if [[ "${current_branch}" == "main" || "${current_branch}" == "master" ]]; then
  fail "direct push from '${current_branch}' is blocked; use topic branch + PR"
fi

if ! git rev-parse --verify HEAD >/dev/null 2>&1; then
  fail "no local HEAD commit found"
fi

if ! git show-ref --verify --quiet refs/remotes/origin/main; then
  fail "origin/main not found locally; run: git fetch origin main"
fi

if ! git merge-base --is-ancestor refs/remotes/origin/main HEAD; then
  fail "current branch does not descend from origin/main (possible unrelated history)"
fi

tracked_bad="$(git ls-files | grep -E "${BAD_PATTERN}" || true)"
if [[ -n "${tracked_bad}" ]]; then
  fail "tracked system metadata detected (._*, .DS_Store, .Spotlight-V100, .Trashes)"
fi

untracked_files="$(git ls-files --others --exclude-standard)"
untracked_bad="$(printf "%s\n" "${untracked_files}" | grep -E "${BAD_PATTERN}" || true)"
if [[ -n "${untracked_bad}" ]]; then
  fail "untracked system metadata detected (cleanup required before push)"
fi

if [[ -n "${untracked_files}" ]]; then
  untracked_count="$(printf "%s\n" "${untracked_files}" | sed '/^$/d' | wc -l | tr -d ' ')"
  if [[ "${untracked_count}" -gt "${MAX_UNTRACKED}" ]]; then
    fail "too many untracked files (${untracked_count} > ${MAX_UNTRACKED}); likely wrong worktree state"
  fi
fi

echo "pre-push sanity check passed"
