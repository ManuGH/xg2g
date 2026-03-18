#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

TARGET_PATH="backend/internal/control/http/dist"

before_diff="$(mktemp)"
after_diff="$(mktemp)"
before_untracked="$(mktemp)"
after_untracked="$(mktemp)"
trap 'rm -f "$before_diff" "$after_diff" "$before_untracked" "$after_untracked"' EXIT

git diff -- "$TARGET_PATH" > "$before_diff"
git ls-files --others --exclude-standard -- "$TARGET_PATH" | LC_ALL=C sort > "$before_untracked"

make ui-build

git diff -- "$TARGET_PATH" > "$after_diff"
git ls-files --others --exclude-standard -- "$TARGET_PATH" | LC_ALL=C sort > "$after_untracked"

if ! cmp -s "$before_diff" "$after_diff" || ! cmp -s "$before_untracked" "$after_untracked"; then
  echo "❌ Embedded WebUI dist drift detected."
  echo "   Run: make ui-build"
  echo ""
  echo "Status for tracked scope:"
  git status --short -- "$TARGET_PATH"
  exit 1
fi

echo "✅ Embedded WebUI dist drift lock passed"
