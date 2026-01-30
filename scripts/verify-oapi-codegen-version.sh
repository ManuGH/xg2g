#!/usr/bin/env bash
set -euo pipefail

EXPECTED="v2.5.1"
LINE=$(rg -n "^# github.com/oapi-codegen/oapi-codegen/v2 " vendor/modules.txt || true)
if [ -z "$LINE" ]; then
  echo "❌ oapi-codegen not found in vendor/modules.txt"
  exit 1
fi

VERSION=$(echo "$LINE" | awk '{print $3}')
if [ "$VERSION" != "$EXPECTED" ]; then
  echo "❌ oapi-codegen version drift: expected $EXPECTED, got $VERSION"
  exit 1
fi

echo "✅ oapi-codegen version pinned ($VERSION)"
