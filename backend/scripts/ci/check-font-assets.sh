#!/usr/bin/env bash
set -euo pipefail

# Guards against font files that are not fonts.
#
# A download that follows a redirect to an error page (GitHub 404, rate-limit
# interstitial, SSO login form) still exits 0 and still writes a file, so a
# broken curl/wget can leave an HTML page sitting at res/font/foo.ttf. AAPT2
# packages it happily as an opaque resource, and the failure only surfaces at
# runtime as text silently rendered in the system font instead of the intended
# typeface.
#
# The sfnt magic number is checked directly rather than shelling out to file(1)
# so this behaves identically on macOS and on CI runners.

echo "Checking Android font assets..."

failed=0
checked=0

describe() {
  if command -v file >/dev/null 2>&1; then
    file -b "$1" | cut -c1-60
  else
    echo "unknown type"
  fi
}

while IFS= read -r font; do
  # Font family definitions live alongside the binaries and are meant to be XML.
  case "$font" in
  *.xml) continue ;;
  esac

  checked=$((checked + 1))

  case "$font" in
  *.ttf | *.otf | *.ttc) ;;
  *)
    echo "ERROR: $font"
    echo "  Unexpected extension. Android res/font accepts .ttf, .otf, .ttc and .xml only."
    failed=1
    continue
    ;;
  esac

  magic="$(od -A n -t x1 -N 4 "$font" | tr -d ' \n')"

  case "$magic" in
  # TrueType outlines, Apple 'true', OpenType/CFF 'OTTO', collection 'ttcf'.
  00010000 | 74727565 | 4f54544f | 74746366) ;;
  774f4646 | 774f4632)
    echo "ERROR: $font"
    echo "  This is a WOFF/WOFF2 web font; Android needs the raw .ttf or .otf."
    failed=1
    ;;
  *)
    echo "ERROR: $font"
    echo "  Not a font. Leading bytes: ${magic:-<empty file>} ($(describe "$font"))"
    echo "  This is usually a download that landed on an error page instead of the"
    echo "  real file. Re-fetch it and confirm the bytes before committing."
    failed=1
    ;;
  esac
done < <(find . -type d -path "*/src/main/res/font" \
  -not -path "*/build/*" \
  -not -path "*/node_modules/*" \
  -not -path "*/.*/*" \
  -exec find {} -maxdepth 1 -type f \; | sort)

if [[ $failed -ne 0 ]]; then
  echo
  echo "One or more font assets are not real fonts."
  exit 1
fi

echo "OK: $checked font asset(s) verified"
