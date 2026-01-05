#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "$ROOT/go.mod" ]]; then
  echo "go.mod not found" >&2
  exit 1
fi

# Source of truth: go directive in go.mod (minor-pinned policy).
go_version="$(awk '/^go[[:space:]]+[0-9]+\.[0-9]+/ {print $2; exit}' "$ROOT/go.mod")"
if [[ -z "$go_version" ]]; then
  echo "failed to read go version from go.mod" >&2
  exit 1
fi

fail=0

check_dockerfile() {
  local file="$1"
  local expected="$2"
  if [[ ! -f "$file" ]]; then
    echo "missing $file" >&2
    fail=1
    return
  fi
  local tags
  tags=$(grep -E '^FROM[[:space:]]+golang:' "$file" | awk '{print $2}' || true)
  if [[ -z "$tags" ]]; then
    echo "no golang base image found in $file" >&2
    fail=1
    return
  fi
  while read -r tag; do
    [[ -z "$tag" ]] && continue
    local version
    version="${tag#golang:}"
    version="${version%%-*}"
    if [[ "$version" != "$expected" ]]; then
      echo "${file}: golang base version '$version' does not match go.mod '$expected'" >&2
      fail=1
    fi
  done <<< "$tags"
}

check_workflows() {
  local expected="$1"
  local wf
  local bad=0
  while IFS= read -r -d '' wf; do
    local versions
    versions=$(grep -E '^[[:space:]]*go-version:[[:space:]]*' "$wf" | \
      sed -E 's/^[[:space:]]*go-version:[[:space:]]*"?([^" ]+)"?.*/\1/' || true)
    if [[ -n "$versions" ]]; then
      while read -r v; do
        [[ -z "$v" ]] && continue
        if [[ "$v" != "$expected" ]]; then
          echo "${wf}: go-version '$v' does not match go.mod '$expected'" >&2
          bad=1
        fi
      done <<< "$versions"
    fi

    local files
    files=$(grep -E '^[[:space:]]*go-version-file:[[:space:]]*' "$wf" | \
      sed -E 's/^[[:space:]]*go-version-file:[[:space:]]*"?([^" ]+)"?.*/\1/' || true)
    if [[ -n "$files" ]]; then
      while read -r f; do
        [[ -z "$f" ]] && continue
        if [[ "$f" != "go.mod" ]]; then
          echo "${wf}: go-version-file '$f' must be go.mod" >&2
          bad=1
        fi
      done <<< "$files"
    fi
  done < <(find "$ROOT/.github/workflows" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
  if [[ "$bad" -ne 0 ]]; then
    fail=1
  fi
}

check_dockerfile "$ROOT/Dockerfile" "$go_version"
check_dockerfile "$ROOT/Dockerfile.distroless" "$go_version"
check_workflows "$go_version"

if [[ "$fail" -ne 0 ]]; then
  exit 1
fi

echo "Go toolchain policy OK (go.mod: $go_version)"
