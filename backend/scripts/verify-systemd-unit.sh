#!/usr/bin/env bash
set -euo pipefail

CANONICAL_UNIT="deploy/xg2g.service"
COMPAT_UNIT="docs/ops/xg2g.service"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

if [ -f xg2g.service ]; then
  fail "repo root xg2g.service must not exist; canonical unit is ${CANONICAL_UNIT}"
fi

[[ -f "${CANONICAL_UNIT}" ]] || fail "missing canonical unit at ${CANONICAL_UNIT}"
[[ -f "${COMPAT_UNIT}" ]] || fail "missing compatibility mirror at ${COMPAT_UNIT}"

tmp_canonical="$(mktemp)"
tmp_compat="$(mktemp)"
trap 'rm -f "${tmp_canonical}" "${tmp_compat}"' EXIT

tail -n +2 "${CANONICAL_UNIT}" > "${tmp_canonical}"
tail -n +2 "${COMPAT_UNIT}" > "${tmp_compat}"
diff -u "${tmp_canonical}" "${tmp_compat}" >/dev/null || fail "compatibility mirror drifted: ${COMPAT_UNIT} no longer matches ${CANONICAL_UNIT}"

echo "OK: canonical deploy unit present, compatibility mirror is in sync, and no duplicate exists"
