#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# The generated @hey-api/openapi-ts TypeScript client must stay in lock-step with
# the OpenAPI contract (backend/api/openapi.yaml). Regenerate and fail on drift —
# mirrors verify-embedded-webui-dist for the embedded WebUI bundle.
TARGET_PATH="frontend/webui/src/client-ts"

before_diff="$(mktemp)"
after_diff="$(mktemp)"
before_untracked="$(mktemp)"
after_untracked="$(mktemp)"
trap 'rm -f "$before_diff" "$after_diff" "$before_untracked" "$after_untracked"' EXIT

git diff -- "$TARGET_PATH" > "$before_diff"
git ls-files --others --exclude-standard -- "$TARGET_PATH" | LC_ALL=C sort > "$before_untracked"

"${MAKE:-make}" generate-client

git diff -- "$TARGET_PATH" > "$after_diff"
git ls-files --others --exclude-standard -- "$TARGET_PATH" | LC_ALL=C sort > "$after_untracked"

if ! cmp -s "$before_diff" "$after_diff" || ! cmp -s "$before_untracked" "$after_untracked"; then
  echo "❌ Generated TS API client drift detected."
  echo "   frontend/webui/src/client-ts is out of sync with backend/api/openapi.yaml."
  echo "   Run: make generate-client   (then commit the regenerated client)"
  echo ""
  echo "Status for tracked scope:"
  git status --short -- "$TARGET_PATH"
  exit 1
fi

echo "✅ Generated TS API client drift lock passed"
