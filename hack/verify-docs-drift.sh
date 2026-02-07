#!/usr/bin/env bash
set -euo pipefail

fail() { echo "ERROR: $*" >&2; exit 1; }

# Ensure clean working tree (including untracked) to make drift detection meaningful.
if [[ -n "$(git status --porcelain)" ]]; then
  fail "Working tree is dirty (or has untracked files). Commit/stash/clean before running docs drift check."
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

# Fail if docs render produced changes
git diff --exit-code

echo "OK: docs render produced no drift."
