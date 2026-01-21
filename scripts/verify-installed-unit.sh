#!/usr/bin/env bash
set -euo pipefail

UNIT_NAME="xg2g.service"
EXPECTED_UNIT="${1:-/srv/xg2g/docs/ops/xg2g.service}"

DROPIN_DIR="/etc/systemd/system/${UNIT_NAME}.d"
if [ -d "$DROPIN_DIR" ] && [ "$(ls -A "$DROPIN_DIR" 2>/dev/null)" ]; then
  echo "ERROR: Drop-ins detected at $DROPIN_DIR (forbidden). Contents:" >&2
  ls -la "$DROPIN_DIR" >&2 || true
  exit 1
fi

if ! systemctl cat "$UNIT_NAME" >/dev/null 2>&1; then
  echo "ERROR: Installed unit not found: $UNIT_NAME" >&2
  exit 1
fi

if [ ! -f "$EXPECTED_UNIT" ]; then
  echo "ERROR: Expected unit file not found: $EXPECTED_UNIT" >&2
  exit 1
fi

tmp_installed="$(mktemp)"
tmp_expected="$(mktemp)"
trap 'rm -f "$tmp_installed" "$tmp_expected"' EXIT

systemctl cat "$UNIT_NAME" \
  | sed -E '/^# \/.*\.d\/.*\.conf$/d' \
  | sed -E '/^# \/.*\/'"$UNIT_NAME"'$/d' \
  > "$tmp_installed"

cp "$EXPECTED_UNIT" "$tmp_expected"

if ! diff -u "$tmp_expected" "$tmp_installed" >/dev/null; then
  echo "ERROR: Installed $UNIT_NAME drifted from expected unit: $EXPECTED_UNIT" >&2
  echo "---- diff ----" >&2
  diff -u "$tmp_expected" "$tmp_installed" >&2 || true
  exit 1
fi

echo "OK: Installed unit matches expected and no drop-ins exist."
