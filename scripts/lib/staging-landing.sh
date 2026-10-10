#!/usr/bin/env bash

# Staging landing guard shared by the staging fast-deploy and its contract
# verifier.
#
# A staging deploy replaces whatever runs on :8089. When that build never reached
# main and is not part of an open or merged pull request, the deploy silently
# drops tested work: on 2026-10-10 a receiver transcoder and a set of iOS
# playback fixes existed only as staging builds of PR-less branches, and the next
# deploy from another branch removed them without anyone noticing. These pure
# functions classify the running build before it is replaced, and the build
# about to be deployed, so the deploy refuses instead of losing work.

# staging_pr_states_for_commit prints the state (OPEN, MERGED or CLOSED) of every
# pull request that contains the commit, one per line. Search covers commits
# anywhere in a pull request, not only its head; it is indexed with a short delay,
# so a commit pushed seconds ago may not be found yet. The verifier overrides it.
staging_pr_states_for_commit() {
  local commit="$1"
  gh pr list --state all --search "${commit}" --json state --jq '.[].state'
}

# staging_open_pr_heads prints the head commit of every open pull request, one
# per line. Unlike search it has no indexing delay. The verifier overrides it.
staging_open_pr_heads() {
  gh pr list --state open --limit 200 --json headRefOid --jq '.[].headRefOid'
}

# classify_staging_replacement <repo> <running_commit> <new_commit> <main_ref>
#
# Prints one of:
#   none           nothing identifiable runs on staging
#   same           the new build is the running commit
#   descendant     the new build contains the running commit
#   in_main        the running commit is already on main
#   in_pr          the running commit is part of an open or merged pull request
#   unlanded       none of the above: replacing it would lose the work
#   lookup_failed  GitHub could not be asked, so landing is unknown
# Returns 0 when replacing the running build loses nothing, 1 otherwise.
classify_staging_replacement() {
  local repo="$1"
  local running="$2"
  local new="$3"
  local main_ref="$4"
  local running_full new_full heads states

  if [[ -z "${running}" ]]; then
    printf 'none\n'
    return 0
  fi
  if [[ ! "${running}" =~ ^[0-9a-fA-F]{7,40}$ ]]; then
    printf 'unlanded\n'
    return 1
  fi

  new_full="$(git -C "${repo}" rev-parse --verify --quiet "${new}^{commit}")" || {
    printf 'unlanded\n'
    return 1
  }
  running_full="$(git -C "${repo}" rev-parse --verify --quiet "${running}^{commit}" 2>/dev/null || true)"
  if [[ -n "${running_full}" ]]; then
    if [[ "${running_full}" == "${new_full}" ]]; then
      printf 'same\n'
      return 0
    fi
    if git -C "${repo}" merge-base --is-ancestor "${running_full}" "${new_full}"; then
      printf 'descendant\n'
      return 0
    fi
    if git -C "${repo}" merge-base --is-ancestor "${running_full}" "${main_ref}"; then
      printf 'in_main\n'
      return 0
    fi
  fi

  if ! heads="$(staging_open_pr_heads)"; then
    printf 'lookup_failed\n'
    return 1
  fi
  if [[ -n "${running_full}" ]] && grep -qxF -- "${running_full}" <<<"${heads}"; then
    printf 'in_pr\n'
    return 0
  fi
  if ! states="$(staging_pr_states_for_commit "${running_full:-${running}}")"; then
    printf 'lookup_failed\n'
    return 1
  fi
  if grep -qxE 'OPEN|MERGED' <<<"${states}"; then
    printf 'in_pr\n'
    return 0
  fi
  printf 'unlanded\n'
  return 1
}

# classify_staging_candidate <repo> <new_commit> <main_ref>
#
# The build about to be deployed must already be on main or be the head of an
# open pull request (a draft is fine), so that it cannot become the next
# unlanded staging build. Prints in_main, in_open_pr, no_pr or lookup_failed.
classify_staging_candidate() {
  local repo="$1"
  local new="$2"
  local main_ref="$3"
  local new_full heads

  new_full="$(git -C "${repo}" rev-parse --verify --quiet "${new}^{commit}")" || {
    printf 'no_pr\n'
    return 1
  }
  if git -C "${repo}" merge-base --is-ancestor "${new_full}" "${main_ref}"; then
    printf 'in_main\n'
    return 0
  fi
  if ! heads="$(staging_open_pr_heads)"; then
    printf 'lookup_failed\n'
    return 1
  fi
  if grep -qxF -- "${new_full}" <<<"${heads}"; then
    printf 'in_open_pr\n'
    return 0
  fi
  printf 'no_pr\n'
  return 1
}
