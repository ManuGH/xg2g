#!/usr/bin/env bash
set -euo pipefail

command -v curl >/dev/null || { echo "FAIL: curl not found"; exit 1; }
command -v jq >/dev/null || { echo "FAIL: jq not found"; exit 1; }
command -v awk >/dev/null || { echo "FAIL: awk not found"; exit 1; }

PROM_URL="${PROM_URL:-http://prometheus:9090}"
THRESHOLD="${THRESHOLD:-0.001}"       # 0.1%
MIN_SAMPLES="${MIN_SAMPLES:-1000}"    # guard against low-volume windows
WINDOW="${WINDOW:-14d}"
INTENTS_PATH_LABEL="${INTENTS_PATH_LABEL:-/api/v3/intents}"
INTENTS_TYPE_LABEL="${INTENTS_TYPE_LABEL:-stream.start}"

Q_NUM="sum(increase(xg2g_live_intents_playback_key_total{key=\"playback_decision_id\",result=\"accepted\"}[${WINDOW}]))"
Q_DEN="sum(increase(xg2g_live_intents_total{path=\"${INTENTS_PATH_LABEL}\",type=\"${INTENTS_TYPE_LABEL}\"}[${WINDOW}]))"

num="$(curl -fsS --get "${PROM_URL}/api/v1/query" --data-urlencode "query=${Q_NUM}" \
  | jq -r 'first(.data.result[]?.value[1]) // "0"')"
den="$(curl -fsS --get "${PROM_URL}/api/v1/query" --data-urlencode "query=${Q_DEN}" \
  | jq -r 'first(.data.result[]?.value[1]) // "0"')"

awk -v n="${num}" -v d="${den}" -v t="${THRESHOLD}" -v m="${MIN_SAMPLES}" '
BEGIN {
  if (d + 0 < m + 0) {
    print "FAIL: insufficient samples (" d ")";
    exit 1;
  }
  ratio = (n + 0) / (d + 0);
  if (ratio >= t + 0) {
    printf "FAIL: alias ratio %.6f >= %.6f\n", ratio, t;
    exit 1;
  }
  printf "PASS: alias ratio %.6f < %.6f\n", ratio, t;
}'
