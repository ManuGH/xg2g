#!/usr/bin/env bash
set -euo pipefail

CANONICAL_UNIT="deploy/xg2g.service"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

if [ -f xg2g.service ]; then
  fail "repo root xg2g.service must not exist; canonical unit is ${CANONICAL_UNIT}"
fi

[[ -f "${CANONICAL_UNIT}" ]] || fail "missing canonical unit at ${CANONICAL_UNIT}"

echo "OK: canonical deploy unit present and no duplicate exists"
