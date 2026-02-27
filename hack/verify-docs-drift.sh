#!/usr/bin/env bash
set -euo pipefail

fail() { echo "ERROR: $*" >&2; exit 1; }

ALLOW_DIRTY=0
if [[ "${1:-}" == "--allow-dirty" ]]; then
  ALLOW_DIRTY=1
  shift
fi

if [[ "$#" -ne 0 ]]; then
  fail "Usage: $0 [--allow-dirty]"
fi

rendered_paths=(
  "README.md"
  "docker-compose.yml"
  "docs/ops/xg2g.service"
  "docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md"
  "docs/ops/OPERATIONS_MODEL.md"
  "docs/ops/xg2g-verifier.service"
  "docs/ops/xg2g-verifier.timer"
)

snapshot_rendered_state() {
  local out_file="$1"
  : > "$out_file"
  local path hash
  for path in "${rendered_paths[@]}"; do
    if [[ -f "$path" ]]; then
      hash="$(git hash-object "$path")"
      printf "%s\t%s\n" "$path" "$hash" >> "$out_file"
    else
      printf "%s\t%s\n" "$path" "MISSING" >> "$out_file"
    fi
  done
}

if [[ "$ALLOW_DIRTY" -eq 0 ]]; then
  # Ensure clean working tree (including untracked) to make drift detection meaningful.
  if [[ -n "$(git status --porcelain)" ]]; then
    fail "Working tree is dirty (or has untracked files). Commit/stash/clean or run with --allow-dirty."
  fi
fi

before_snapshot=""
after_snapshot=""
if [[ "$ALLOW_DIRTY" -eq 1 ]]; then
  before_snapshot="$(mktemp)"
  after_snapshot="$(mktemp)"
  trap 'rm -f "$before_snapshot" "$after_snapshot"' EXIT
  snapshot_rendered_state "$before_snapshot"
fi

# Prefer project script if present
if [[ -x "./scripts/render-docs.sh" ]]; then
  ./scripts/render-docs.sh
elif [[ -x "./render-docs.sh" ]]; then
  ./render-docs.sh
elif command -v make >/dev/null 2>&1; then
  make docs-render
else
  fail "No scripts/render-docs.sh, render-docs.sh, or make docs-render found."
fi

if [[ "$ALLOW_DIRTY" -eq 0 ]]; then
  # Fail if docs render produced changes
  git diff --exit-code
else
  snapshot_rendered_state "$after_snapshot"
  if ! diff -u "$before_snapshot" "$after_snapshot" >/dev/null; then
    fail "Docs drift detected in rendered targets while running in --allow-dirty mode."
  fi
fi

echo "OK: docs render produced no drift."
