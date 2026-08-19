#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# The generated native contract must stay in lock-step with the OpenAPI document
# (backend/api/openapi.yaml). Regenerate and fail on drift — mirrors
# verify-client-ts-fresh for the WebUI client.
#
# The iOS and Android models used to be hand-maintained, which is how the
# pairing exchange ended up decoded three different ways in three clients. This
# gate is what keeps that from being possible again.
TARGET_PATHS=(
  "ios/Xg2g/Generated"
  "android/app/src/main/java/io/github/manugh/xg2g/android/contract"
)

before_diff="$(mktemp)"
after_diff="$(mktemp)"
before_untracked="$(mktemp)"
after_untracked="$(mktemp)"
trap 'rm -f "$before_diff" "$after_diff" "$before_untracked" "$after_untracked"' EXIT

git diff -- "${TARGET_PATHS[@]}" > "$before_diff"
git ls-files --others --exclude-standard -- "${TARGET_PATHS[@]}" | LC_ALL=C sort > "$before_untracked"

"${MAKE:-make}" generate-native-contract

git diff -- "${TARGET_PATHS[@]}" > "$after_diff"
git ls-files --others --exclude-standard -- "${TARGET_PATHS[@]}" | LC_ALL=C sort > "$after_untracked"

if ! cmp -s "$before_diff" "$after_diff" || ! cmp -s "$before_untracked" "$after_untracked"; then
  echo "❌ Generated native contract drift detected."
  echo "   The iOS/Android models are out of sync with backend/api/openapi.yaml."
  echo "   Run: make generate-native-contract   (then commit the regenerated models)"
  echo ""
  echo "Status for tracked scope:"
  git status --short -- "${TARGET_PATHS[@]}"
  exit 1
fi

echo "✅ Generated native contract drift lock passed"
