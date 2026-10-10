#!/usr/bin/env bash
set -euo pipefail

# Builds and archives the xg2g iOS/iPadOS Release bundle for App Store Connect / TestFlight submission.
# Usage:
#   ./ios/scripts/archive-release.sh [--platform ios|tvos] [--team-id TEAM_ID]

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROJECT_PATH="${ROOT_DIR}/ios/Xg2g.xcodeproj"
ARCHIVE_DIR="${ROOT_DIR}/ios/build/archives"
PLATFORM="ios"
TEAM_ID=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform)
      PLATFORM="$2"
      shift 2
      ;;
    --team-id)
      TEAM_ID="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--platform ios|tvos] [--team-id TEAM_ID]" >&2
      exit 1
      ;;
  esac
done

mkdir -p "${ARCHIVE_DIR}"
TIMESTAMP="$(date +%Y%m%d-%H%M%S)"

if [[ "${PLATFORM}" == "ios" ]]; then
  SCHEME="Xg2g"
  DESTINATION="generic/platform=iOS"
  ARCHIVE_PATH="${ARCHIVE_DIR}/Xg2g-iOS-${TIMESTAMP}.xcarchive"
elif [[ "${PLATFORM}" == "tvos" ]]; then
  SCHEME="Xg2g-tvOS"
  DESTINATION="generic/platform=tvOS"
  ARCHIVE_PATH="${ARCHIVE_DIR}/Xg2g-tvOS-${TIMESTAMP}.xcarchive"
else
  echo "Unsupported platform '${PLATFORM}'. Use 'ios' or 'tvos'." >&2
  exit 1
fi

echo "==> Validating PrivacyInfo.xcprivacy and Info.plist keys..."
plutil -lint "${ROOT_DIR}/ios/Xg2g/PrivacyInfo.xcprivacy"
plutil -lint "${ROOT_DIR}/ios/Support/Info.plist"

EXTRA_ARGS=(-allowProvisioningUpdates)
if [[ -n "${TEAM_ID}" ]]; then
  EXTRA_ARGS+=("DEVELOPMENT_TEAM=${TEAM_ID}")
fi

echo "==> Archiving ${SCHEME} (${DESTINATION}) -> ${ARCHIVE_PATH}"
xcodebuild archive \
  -project "${PROJECT_PATH}" \
  -scheme "${SCHEME}" \
  -configuration Release \
  -destination "${DESTINATION}" \
  -archivePath "${ARCHIVE_PATH}" \
  "${EXTRA_ARGS[@]}"

echo "==> Archive created successfully at:"
echo "    ${ARCHIVE_PATH}"
echo ""
echo "Next step: Open in Xcode Organizer to upload to App Store Connect / TestFlight:"
echo "    open \"${ARCHIVE_PATH}\""
