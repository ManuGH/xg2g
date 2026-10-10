#!/usr/bin/env bash
set -euo pipefail

# Automatically re-signs and refreshes the xg2g iOS app on Manuel's iPhone over Wi-Fi
# so the 7-day free developer certificate never expires.

DEVICE_ID="00008150-00064CC10EFB801C"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_FILE="${HOME}/Library/Logs/xg2g-refresh.log"
mkdir -p "$(dirname "${LOG_FILE}")"

timestamp() { date "+%Y-%m-%d %H:%M:%S"; }

echo "[$(timestamp)] Checking availability for iPhone (${DEVICE_ID})..." >> "${LOG_FILE}"

# Check if the device is reachable on the local network / paired
if ! xcrun devicectl list devices 2>/dev/null | grep -q "${DEVICE_ID}.*available"; then
  echo "[$(timestamp)] iPhone not reachable or locked right now. Skipping refresh until next cycle." >> "${LOG_FILE}"
  exit 0
fi

echo "[$(timestamp)] iPhone is available! Re-signing and deploying fresh 7-day build..." >> "${LOG_FILE}"

if xcodebuild -project "${ROOT_DIR}/ios/Xg2g.xcodeproj" \
    -scheme Xg2g \
    -destination "id=${DEVICE_ID}" \
    -allowProvisioningUpdates \
    build -quiet >> "${LOG_FILE}" 2>&1; then

  APP_PATH="/Users/manuel/Library/Developer/Xcode/DerivedData/Xg2g-fqxcaozuilikxabxpcvdihknqdpr/Build/Products/Debug-iphoneos/Xg2g.app"
  if xcrun devicectl device install app --device "${DEVICE_ID}" "${APP_PATH}" >> "${LOG_FILE}" 2>&1; then
    echo "[$(timestamp)] ✅ Successfully refreshed xg2g on iPhone! Next 7-day cycle active." >> "${LOG_FILE}"
  else
    echo "[$(timestamp)] ⚠️ Build succeeded but install was skipped (device possibly locked with passcode)." >> "${LOG_FILE}"
  fi
else
  echo "[$(timestamp)] ❌ xcodebuild failed during automated refresh." >> "${LOG_FILE}"
fi
