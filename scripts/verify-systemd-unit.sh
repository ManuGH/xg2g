#!/bin/sh
set -eu

if [ -f xg2g.service ]; then
  echo "ERROR: repo root xg2g.service must not exist; canonical unit is docs/ops/xg2g.service" >&2
  exit 1
fi

if [ ! -f docs/ops/xg2g.service ]; then
  echo "ERROR: missing canonical unit at docs/ops/xg2g.service" >&2
  exit 1
fi

echo "OK: canonical systemd unit present and no duplicate exists"
