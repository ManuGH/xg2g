#!/usr/bin/env bash
set -euo pipefail

# Builds signed Release APKs for Android Smartphones/Tablets, Android TV, and Amazon Fire TV.
# Works for 0 € without any paid developer account and can optionally upload to GitHub Releases.
#
# Usage:
#   ./android/scripts/build-apk.sh [--publish-release <TAG>]

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
ANDROID_DIR="${ROOT_DIR}/android"
OUT_DIR="${ANDROID_DIR}/build/apk"
KEYSTORE_DIR="${ANDROID_DIR}/build/keystore"
PUBLISH_TAG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --publish-release)
      PUBLISH_TAG="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      echo "Usage: $0 [--publish-release <TAG>]" >&2
      exit 1
      ;;
  esac
done

mkdir -p "${OUT_DIR}" "${KEYSTORE_DIR}"

# Resolve JAVA_HOME (fall back to Android Studio's bundled JBR on macOS if needed)
if [[ -z "${JAVA_HOME:-}" ]]; then
  for candidate in \
    "/Applications/Android Studio.app/Contents/jbr/Contents/Home" \
    "$HOME/Applications/Android Studio.app/Contents/jbr/Contents/Home"; do
    if [[ -x "${candidate}/bin/java" ]]; then
      export JAVA_HOME="${candidate}"
      export PATH="${JAVA_HOME}/bin:${PATH}"
      break
    fi
  done
fi

# Ensure a valid signing keystore is available
if [[ -z "${KEYSTORE_FILE:-}" ]]; then
  export KEYSTORE_FILE="${KEYSTORE_DIR}/xg2g-release.p12"
  export KEYSTORE_PASSWORD="${KEYSTORE_PASSWORD:-xg2g-open-release-key}"
  export KEY_ALIAS="${KEY_ALIAS:-xg2g}"
  export KEY_PASSWORD="${KEY_PASSWORD:-${KEYSTORE_PASSWORD}}"

  if [[ ! -f "${KEYSTORE_FILE}" ]]; then
    echo "==> Generating local release signing keystore at ${KEYSTORE_FILE}..."
    keytool -genkeypair \
      -v \
      -keystore "${KEYSTORE_FILE}" \
      -storetype PKCS12 \
      -storepass "${KEYSTORE_PASSWORD}" \
      -keypass "${KEY_PASSWORD}" \
      -alias "${KEY_ALIAS}" \
      -keyalg RSA \
      -keysize 2048 \
      -validity 10000 \
      -dname "CN=xg2g, OU=OpenSource, O=ManuGH, L=Home, ST=DE, C=DE" >/dev/null 2>&1
  fi
fi

echo "==> Building signed Android / Android TV / Fire TV Release APKs..."
(
  cd "${ANDROID_DIR}"
  ./gradlew :app:assembleProdRelease :app:assembleStagingRelease --quiet
)

PROD_APK="${ANDROID_DIR}/app/build/outputs/apk/prod/release/app-prod-release.apk"
STAGING_APK="${ANDROID_DIR}/app/build/outputs/apk/staging/release/app-staging-release.apk"

if [[ ! -f "${PROD_APK}" ]]; then
  echo "❌ Expected prod release APK not found at ${PROD_APK}" >&2
  exit 1
fi

# Standardized professional platform names
cp "${PROD_APK}" "${OUT_DIR}/xg2g-android.apk"
shasum -a 256 "${OUT_DIR}/xg2g-android.apk" | awk '{print $1 "  xg2g-android.apk"}' > "${OUT_DIR}/xg2g-android.apk.sha256"

cp "${PROD_APK}" "${OUT_DIR}/xg2g-android-tv.apk"
shasum -a 256 "${OUT_DIR}/xg2g-android-tv.apk" | awk '{print $1 "  xg2g-android-tv.apk"}' > "${OUT_DIR}/xg2g-android-tv.apk.sha256"

# Also provide convenient device-specific symlinks/copies so non-technical users find their search term immediately
cp "${PROD_APK}" "${OUT_DIR}/xg2g-firetv.apk"
shasum -a 256 "${OUT_DIR}/xg2g-firetv.apk" | awk '{print $1 "  xg2g-firetv.apk"}' > "${OUT_DIR}/xg2g-firetv.apk.sha256"

echo "✅ Created standardized release APKs in ${OUT_DIR}:"
echo "   - xg2g-android.apk    (Standard: Android Smartphones & Tablets)"
echo "   - xg2g-android-tv.apk (Standard: Android TV, Google TV & Amazon Fire TV)"
echo "   - xg2g-firetv.apk     (Direkt für Fire TV Suche)"

if [[ -f "${STAGING_APK}" ]]; then
  cp "${STAGING_APK}" "${OUT_DIR}/xg2g-android-lan.apk"
  cp "${STAGING_APK}" "${OUT_DIR}/xg2g-android-tv-lan.apk"
  echo "   - *-lan.apk           (Erlaubt unverschlüsseltes HTTP im Heimnetz)"
fi

if [[ -n "${PUBLISH_TAG}" ]]; then
  echo "==> Uploading Android & Fire TV APKs to GitHub Release '${PUBLISH_TAG}'..."
  FILES_TO_UPLOAD=()
  for f in "${OUT_DIR}"/*.apk "${OUT_DIR}"/*.sha256; do
    [[ -f "$f" ]] && FILES_TO_UPLOAD+=("$f")
  done

  if gh release view "${PUBLISH_TAG}" >/dev/null 2>&1; then
    gh release upload "${PUBLISH_TAG}" "${FILES_TO_UPLOAD[@]}" --clobber
  else
    gh release create "${PUBLISH_TAG}" "${FILES_TO_UPLOAD[@]}" \
      --title "xg2g ${PUBLISH_TAG}" \
      --notes "Client packages for iOS/tvOS (.ipa) and Android/Android TV/Fire TV (.apk)."
  fi
  echo "🚀 Uploaded Android/Fire TV APKs to https://github.com/ManuGH/xg2g/releases/tag/${PUBLISH_TAG}"
fi
