#!/usr/bin/env bash
#
# Canonical script to execute the iOS unit and integration test suite on simulator.
#
# Usage: ios/scripts/run-ios-tests.sh [simulator name/ID] [extra-xcodebuild-flags...]

set -euo pipefail

SIMULATOR="${1:-iPhone 18 Pro}"
shift 2>/dev/null || true
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if ! command -v xcodebuild >/dev/null 2>&1; then
  if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "ℹ️  xcodebuild not available on $(uname -s); skipping iOS tests"
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

echo "==> Running iOS unit and integration tests on ${DESTINATION}..."

# Boot simulator if not booted to avoid first-run launch delays
xcrun simctl boot "${SIMULATOR}" 2>/dev/null || true

# Note: BackendContractTests runs via run-contract-tests.sh with live server fixture.
# We skip BackendContractTests here if no server fixture is running to allow fast, standalone unit runs.
xcodebuild test \
  -project "${REPO_ROOT}/ios/Xg2g.xcodeproj" \
  -scheme Xg2g \
  -destination "${DESTINATION}" \
  -parallel-testing-enabled NO \
  -skip-testing:Xg2gTests/BackendContractTests \
  "$@"

echo "✅ iOS unit tests passed on ${DESTINATION}"
