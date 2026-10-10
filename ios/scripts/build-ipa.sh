#!/usr/bin/env bash
set -euo pipefail

# Builds sideload-ready .ipa packages (0 € / no paid Apple Developer account required)
# for AltStore, SideStore, Sideloadly, and GitHub Releases.
#
# Usage:
#   ./ios/scripts/build-ipa.sh [--platform ios|tvos|all] [--publish-release <TAG>]

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
PROJECT_PATH="${ROOT_DIR}/ios/Xg2g.xcodeproj"
OUT_DIR="${ROOT_DIR}/ios/build/ipa"
PLATFORM="all"
PUBLISH_TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --platform)
      PLATFORM="$2"
      shift 2
      ;;
    --publish-release)
      PUBLISH_TAG="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--platform ios|tvos|all] [--publish-release <TAG>]" >&2
      exit 1
      ;;
  esac
done

mkdir -p "${OUT_DIR}"

build_single_ipa() {
  local plat="$1"
  local scheme=""
  local destination=""
  local product_dir=""
  local app_name=""
  local ipa_name=""

  if [[ "${plat}" == "ios" ]]; then
    scheme="Xg2g"
    destination="generic/platform=iOS"
    product_dir="Release-iphoneos"
    app_name="Xg2g.app"
    ipa_name="xg2g-ios.ipa"
  elif [[ "${plat}" == "tvos" ]]; then
    scheme="Xg2g-tvOS"
    destination="generic/platform=tvOS"
    product_dir="Release-appletvos"
    app_name="Xg2g-tvOS.app"
    ipa_name="xg2g-tvos.ipa"
  else
    echo "Unsupported platform: ${plat}" >&2
    exit 1
  fi

  local derived_dir="${OUT_DIR}/DerivedData-${plat}"
  local staging_dir="${OUT_DIR}/staging-${plat}"
  local ipa_path="${OUT_DIR}/${ipa_name}"

  rm -rf "${staging_dir}" "${ipa_path}"
  mkdir -p "${staging_dir}/Payload"

  echo "==> Building ${scheme} (${destination}) in Release mode (unsigned for Sideloading)..."
  xcodebuild build \
    -project "${PROJECT_PATH}" \
    -scheme "${scheme}" \
    -configuration Release \
    -destination "${destination}" \
    -derivedDataPath "${derived_dir}" \
    CODE_SIGN_IDENTITY="" \
    CODE_SIGNING_REQUIRED=NO \
    CODE_SIGNING_ALLOWED=NO \
    -quiet

  local built_app="${derived_dir}/Build/Products/${product_dir}/${app_name}"
  if [[ ! -d "${built_app}" ]]; then
    echo "❌ Expected app bundle not found at ${built_app}" >&2
    exit 1
  fi

  echo "==> Packaging ${app_name} into ${ipa_name}..."
  ditto "${built_app}" "${staging_dir}/Payload/${app_name}"

  # Apply ad-hoc signature so local inspection tools and sideloaders see a valid Mach-O signature block
  codesign --force --deep --sign - "${staging_dir}/Payload/${app_name}" >/dev/null 2>&1 || true

  ditto -c -k --sequesterRsrc --keepParent "${staging_dir}/Payload" "${ipa_path}"
  rm -rf "${staging_dir}" "${derived_dir}"

  shasum -a 256 "${ipa_path}" | awk '{print $1 "  " "'"${ipa_name}"'"}' > "${ipa_path}.sha256"
  local size_bytes
  size_bytes="$(stat -f%z "${ipa_path}")"
  echo "✅ Created ${ipa_path} (${size_bytes} bytes)"

  # Create friendly user-facing alias names so users immediately recognize their device
  if [[ "${plat}" == "ios" ]]; then
    cp -f "${ipa_path}" "${OUT_DIR}/Xg2g-iPhone-iPad.ipa"
    cp -f "${ipa_path}.sha256" "${OUT_DIR}/Xg2g-iPhone-iPad.ipa.sha256"
  elif [[ "${plat}" == "tvos" ]]; then
    cp -f "${ipa_path}" "${OUT_DIR}/Xg2g-AppleTV.ipa"
    cp -f "${ipa_path}.sha256" "${OUT_DIR}/Xg2g-AppleTV.ipa.sha256"
  fi
}

if [[ "${PLATFORM}" == "all" ]]; then
  build_single_ipa "ios"
  build_single_ipa "tvos"
else
  build_single_ipa "${PLATFORM}"
fi

# Generate AltStore / SideStore source manifest if iOS IPA exists
if [[ -f "${OUT_DIR}/xg2g-ios.ipa" ]]; then
  IOS_SIZE="$(stat -f%z "${OUT_DIR}/xg2g-ios.ipa")"
  IOS_SHA256="$(awk '{print $1}' "${OUT_DIR}/xg2g-ios.ipa.sha256")"
  VERSION_STR="$(defaults read "${ROOT_DIR}/ios/Support/Info.plist" CFBundleShortVersionString 2>/dev/null || echo "1.0.0")"
  if [[ "${VERSION_STR}" == *'$('* ]]; then
    VERSION_STR="1.0.0"
  fi
  TODAY_ISO="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  DOWNLOAD_TAG="${PUBLISH_TAG:-latest}"
  if [[ "${DOWNLOAD_TAG}" == "latest" ]]; then
    DOWNLOAD_URL="https://github.com/ManuGH/xg2g/releases/latest/download/xg2g-ios.ipa"
  else
    DOWNLOAD_URL="https://github.com/ManuGH/xg2g/releases/download/${DOWNLOAD_TAG}/xg2g-ios.ipa"
  fi

  cat > "${OUT_DIR}/altstore-source.json" <<EOF
{
  "name": "xg2g Official Repository",
  "identifier": "io.github.manugh.xg2g.altstore",
  "subtitle": "Native Enigma2 & xg2g Gateway Client for iOS, iPadOS & tvOS",
  "description": "Official sideload repository for xg2g (SideStore & AltStore).",
  "website": "https://github.com/ManuGH/xg2g",
  "apps": [
    {
      "name": "xg2g",
      "bundleIdentifier": "com.xg2g.app",
      "developerName": "Manuel (ManuGH)",
      "subtitle": "Enigma2 Gateway & Live-TV Client",
      "localizedDescription": "Native Live-TV, EPG Guide, DVR Recordings, and Timer Management for your self-hosted xg2g gateway and Enigma2 receiver.",
      "iconURL": "https://raw.githubusercontent.com/ManuGH/xg2g/main/apps/webui/public/favicon.svg",
      "category": "entertainment",
      "versions": [
        {
          "version": "${VERSION_STR}",
          "date": "${TODAY_ISO}",
          "localizedDescription": "Release ${VERSION_STR} for iOS and iPadOS.",
          "downloadURL": "${DOWNLOAD_URL}",
          "size": ${IOS_SIZE},
          "sha256": "${IOS_SHA256}",
          "minOSVersion": "17.0"
        }
      ]
    }
  ]
}
EOF
  echo "✅ Generated AltStore/SideStore source manifest: ${OUT_DIR}/altstore-source.json"
fi

if [[ -n "${PUBLISH_TAG}" ]]; then
  echo "==> Uploading IPA artifacts to GitHub Release '${PUBLISH_TAG}'..."
  FILES_TO_UPLOAD=()
  for f in "${OUT_DIR}"/*.ipa "${OUT_DIR}"/*.sha256 "${OUT_DIR}/altstore-source.json"; do
    [[ -f "$f" ]] && FILES_TO_UPLOAD+=("$f")
  done

  if gh release view "${PUBLISH_TAG}" >/dev/null 2>&1; then
    gh release upload "${PUBLISH_TAG}" "${FILES_TO_UPLOAD[@]}" --clobber
  else
    gh release create "${PUBLISH_TAG}" "${FILES_TO_UPLOAD[@]}" \
      --title "xg2g ${PUBLISH_TAG}" \
      --notes "iOS (`Xg2g-iOS.ipa`) and tvOS (`Xg2g-tvOS.ipa`) sideload packages for SideStore, AltStore, and Sideloadly."
  fi
  echo "🚀 Uploaded IPA packages to https://github.com/ManuGH/xg2g/releases/tag/${PUBLISH_TAG}"
fi
