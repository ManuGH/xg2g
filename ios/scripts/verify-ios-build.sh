#!/usr/bin/env bash
#
# Canonical gate script to verify that the iOS app and test targets compile cleanly.
#
# Usage: ios/scripts/verify-ios-build.sh [simulator name]

set -euo pipefail

SIMULATOR="${1:-iPhone 17 Pro}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if ! command -v xcodebuild >/dev/null 2>&1; then
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "ℹ️  xcodebuild not available on $(uname -s); skipping iOS build check"
    exit 0
  fi
  echo "❌ xcodebuild not found on macOS" >&2
  exit 1
fi

if [[ "${SIMULATOR}" =~ ^[0-9A-Fa-f-]+$ ]]; then
  DESTINATION="platform=iOS Simulator,id=${SIMULATOR}"
else
  DESTINATION="platform=iOS Simulator,name=${SIMULATOR}"
fi

echo "==> Verifying iOS build-for-testing on ${DESTINATION}..."
xcodebuild build-for-testing \
  -project "${REPO_ROOT}/ios/Xg2g.xcodeproj" \
  -scheme Xg2g \
  -destination "${DESTINATION}" \
  -quiet

echo "✅ iOS build-for-testing passed (entire Xg2gTests target compiles cleanly)"
