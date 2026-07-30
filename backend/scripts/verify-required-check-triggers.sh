#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

verify_required_workflow() {
  local relative_path="$1"
  local workflow="${REPO_ROOT}/${relative_path}"
  local result

  [[ -f "${workflow}" ]] || fail "required-check workflow is missing: ${relative_path}"

  result="$(
    awk '
      /^on:$/ {
        in_on = 1
        next
      }
      in_on && /^[^[:space:]]/ {
        in_on = 0
        in_pull_request = 0
      }
      in_on && /^  pull_request:([[:space:]]*|[[:space:]]*\{\})$/ {
        saw_pull_request = 1
        in_pull_request = 1
        next
      }
      in_pull_request && /^  [^[:space:]]/ {
        in_pull_request = 0
      }
      in_pull_request && /^    paths(-ignore)?:/ {
        print "filtered"
      }
      END {
        if (!saw_pull_request) {
          print "missing"
        }
      }
    ' "${workflow}"
  )"

  [[ "${result}" != *missing* ]] ||
    fail "${relative_path} does not trigger on pull_request"
  [[ "${result}" != *filtered* ]] ||
    fail "${relative_path} emits a required check and must not use top-level pull_request path filters"

  printf 'OK: %s reports its required check for every PR\n' "${relative_path}"
}

verify_required_workflow ".github/workflows/ci.yml"
verify_required_workflow ".github/workflows/pr-required-gates.yml"
