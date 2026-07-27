#!/usr/bin/env bash
# Decide whether a workflow's expensive steps are relevant for this event, and emit
# `relevant=true|false` (plus a human-readable reason) for $GITHUB_OUTPUT.
#
# Why this exists instead of a workflow-level `paths:` filter: a filtered workflow
# does not run at all, so it never reports a status. A required status check that
# never reports leaves the PR stuck on "Expected — waiting for status", which makes
# path-filtered workflows unusable as branch-protection gates. Keeping the job
# unconditional and skipping its *steps* means the context is always produced —
# green and cheap when nothing relevant changed.
#
# Usage:
#   BASE_SHA=... HEAD_SHA=... EVENT_NAME=pull_request \
#     resolve-workflow-scope.sh '^backend/.*\.go$' '^mk/' >> "$GITHUB_OUTPUT"
#
# Patterns are extended regular expressions matched against repo-relative paths.

set -euo pipefail

EVENT_NAME="${EVENT_NAME:-}"
BASE_SHA="${BASE_SHA:-}"
HEAD_SHA="${HEAD_SHA:-}"

emit() {
  printf 'relevant=%s\n' "$1"
  printf 'reason=%s\n' "$2"
  printf '%s: %s\n' "$1" "$2" >&2
}

if [ "$#" -eq 0 ]; then
  echo "usage: $0 <regex>..." >&2
  exit 2
fi

# Anything that is not a pull request (push to main, manual dispatch) runs in full:
# there is no meaningful diff base, and these are the runs that must not silently
# narrow their own scope.
if [ "${EVENT_NAME}" != "pull_request" ]; then
  emit true "event ${EVENT_NAME:-unknown} always runs in full"
  exit 0
fi

if [ -z "${BASE_SHA}" ] || [ -z "${HEAD_SHA}" ]; then
  # Fail open: an undetermined diff must not be mistaken for "nothing to do".
  emit true "diff range undetermined (base='${BASE_SHA}' head='${HEAD_SHA}')"
  exit 0
fi

if ! changed="$(git diff --name-only "${BASE_SHA}" "${HEAD_SHA}" 2>/dev/null)"; then
  emit true "git diff failed for ${BASE_SHA}..${HEAD_SHA}"
  exit 0
fi

if [ -z "${changed}" ]; then
  emit false "no files changed between ${BASE_SHA} and ${HEAD_SHA}"
  exit 0
fi

for pattern in "$@"; do
  if printf '%s\n' "${changed}" | grep -qE "${pattern}"; then
    match="$(printf '%s\n' "${changed}" | grep -E "${pattern}" | head -1)"
    emit true "matched ${pattern} (e.g. ${match})"
    exit 0
  fi
done

count="$(printf '%s\n' "${changed}" | wc -l | tr -d ' ')"
emit false "none of ${count} changed files match this workflow's scope"
