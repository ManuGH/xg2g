#!/usr/bin/env bash
set -euo pipefail

MAX_SIZE_MB="${XG2G_MAX_FILE_SIZE_MB:-5}"
MAX_SIZE_BYTES=$((MAX_SIZE_MB * 1024 * 1024))

if [[ "$#" -eq 0 ]]; then
  mapfile -t files < <(git ls-files)
else
  files=("$@")
fi

large_files=""
for file in "${files[@]}"; do
  [[ -z "$file" ]] && continue
  case "$file" in
    backend/vendor/*|backend/testdata/*|vendor/*|testdata/*)
      continue
      ;;
  esac
  if [[ -f "$file" ]]; then
    if stat_output="$(stat -f%z "$file" 2>/dev/null)"; then
      size="$stat_output"
    else
      size="$(stat -c%s "$file")"
    fi
    if [[ "$size" -gt "$MAX_SIZE_BYTES" ]]; then
      size_mb="$(awk "BEGIN { printf \"%.2f\", ${size} / 1024 / 1024 }")"
      large_files="${large_files}  - ${file} (${size_mb}MB)\n"
    fi
  fi
done

if [[ -n "$large_files" ]]; then
  echo "ERROR: Large files detected (>${MAX_SIZE_MB}MB):"
  printf '%b' "$large_files"
  echo
  echo "Solutions:"
  echo "  1. Move to testdata/ (gitignored)"
  echo "  2. Use Git LFS for legitimate large assets"
  echo "  3. Host externally"
  exit 1
fi

echo "OK: no large files detected"
