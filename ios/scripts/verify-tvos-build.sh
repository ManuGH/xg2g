#!/usr/bin/env bash
# Compile the tvOS app using the same gate locally and in GitHub Actions.
# Usage: ios/scripts/verify-tvos-build.sh [Debug|Release|all]
set -euo pipefail

TVOS_CONFIGURATION="${1:-all}"
case "${TVOS_CONFIGURATION}" in
  Debug|Release) TVOS_CONFIGURATIONS=("${TVOS_CONFIGURATION}") ;;
  all) TVOS_CONFIGURATIONS=(Debug Release) ;;
  *) echo "Usage: $0 [Debug|Release|all]" >&2; exit 2 ;;
esac

if ! command -v xcodebuild >/dev/null 2>&1; then
  echo "The tvOS build gate requires macOS and Xcode 27 or newer." >&2
  exit 1
fi

TVOS_SDK_VERSION="$(xcrun --sdk appletvsimulator --show-sdk-version)"
if [[ ! "${TVOS_SDK_VERSION}" =~ ^[0-9]+(\.[0-9]+)*$ ]] || [[ "${TVOS_SDK_VERSION%%.*}" -lt 27 ]]; then
  echo "The tvOS target requires a tvOS Simulator SDK >= 27; found ${TVOS_SDK_VERSION}." >&2
  exit 1
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TVOS_BUILD_DIR="${XG2G_TVOS_DERIVED_DATA:-${REPO_ROOT}/ios/build/tvos}"
xcodebuild -version

for TVOS_CONFIGURATION in "${TVOS_CONFIGURATIONS[@]}"; do
  echo "Building Xg2gTV ${TVOS_CONFIGURATION} for the tvOS Simulator..."
  xcodebuild build \
    -project "${REPO_ROOT}/ios/Xg2g.xcodeproj" \
    -scheme Xg2gTV \
    -configuration "${TVOS_CONFIGURATION}" \
    -destination 'generic/platform=tvOS Simulator' \
    -derivedDataPath "${TVOS_BUILD_DIR}" \
    -quiet
done

echo "tvOS build gate passed (${TVOS_CONFIGURATIONS[*]})."
