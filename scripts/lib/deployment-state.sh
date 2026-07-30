#!/usr/bin/env bash

# Pure deployment-state classification shared by the maintainer status command
# and its contract verifier. The caller remains responsible for health probes.

normalize_deployment_commit() {
  local repo="$1"
  local commit="$2"

  [[ "${commit}" =~ ^[0-9a-fA-F]{7,40}$ ]] || return 1
  git -C "${repo}" rev-parse --verify "${commit}^{commit}" 2>/dev/null
}

classify_deployment_state() {
  local repo="$1"
  local production_commit="$2"
  local production_sha="$3"
  local staging_commit="$4"
  local staging_sha="$5"
  local manifest_mode="$6"
  local manifest_commit="$7"
  local manifest_sha="$8"
  local production_image_id="$9"
  local staging_image_id="${10}"
  local staging_binary_mount="${11}"
  local production_full staging_full manifest_full

  production_full="$(normalize_deployment_commit "${repo}" "${production_commit}")" ||
    {
      printf 'unknown_production_commit\n'
      return 1
    }
  staging_full="$(normalize_deployment_commit "${repo}" "${staging_commit}")" ||
    {
      printf 'unknown_staging_commit\n'
      return 1
    }

  if [[ "${production_full}" == "${staging_full}" ]]; then
    if [[ "${production_sha}" != "${staging_sha}" ]]; then
      printf 'artifact_drift\n'
      return 1
    fi
    if [[ "${production_image_id}" != "${staging_image_id}" ||
      -n "${staging_binary_mount}" ]]; then
      printf 'runtime_drift\n'
      return 1
    fi
    if [[ "${manifest_mode}" != "baseline" ]]; then
      printf 'baseline_marker_stale\n'
      return 1
    fi
    if [[ "${manifest_sha}" != "${staging_sha}" ]]; then
      printf 'baseline_manifest_mismatch\n'
      return 1
    fi
    manifest_full="$(normalize_deployment_commit "${repo}" "${manifest_commit}")" ||
      {
        printf 'baseline_manifest_mismatch\n'
        return 1
      }
    if [[ "${manifest_full}" != "${staging_full}" ]]; then
      printf 'baseline_manifest_mismatch\n'
      return 1
    fi
    printf 'baseline\n'
    return 0
  fi

  if git -C "${repo}" merge-base --is-ancestor "${production_full}" "${staging_full}"; then
    if [[ "${manifest_mode}" != "candidate" ||
      "${manifest_sha}" != "${staging_sha}" ]]; then
      printf 'untracked_candidate\n'
      return 1
    fi
    manifest_full="$(normalize_deployment_commit "${repo}" "${manifest_commit}")" ||
      {
        printf 'untracked_candidate\n'
        return 1
      }
    if [[ "${manifest_full}" != "${staging_full}" ]]; then
      printf 'untracked_candidate\n'
      return 1
    fi
    printf 'candidate\n'
    return 0
  fi

  if git -C "${repo}" merge-base --is-ancestor "${staging_full}" "${production_full}"; then
    printf 'stale\n'
    return 1
  fi

  printf 'diverged\n'
  return 1
}
