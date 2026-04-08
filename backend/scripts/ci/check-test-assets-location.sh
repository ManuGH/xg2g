#!/usr/bin/env bash
set -euo pipefail

echo "Checking for test assets in repository root..."

test_files="$(find . -maxdepth 1 -type f \( \
  -name "*.mp4" -o \
  -name "*.ts" -o \
  -name "*test*.mp4" -o \
  -name "downloaded_*.ts" -o \
  -name "verify_*.ts" \
\) 2>/dev/null || true)"

if [[ -n "$test_files" ]]; then
  echo "ERROR: Test files found in repository root:"
  printf '%s\n' "$test_files"
  echo
  echo "Move these files into testdata/."
  exit 1
fi

echo "OK: no root-level test assets found"
