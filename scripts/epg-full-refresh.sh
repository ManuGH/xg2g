#!/usr/bin/env bash
# SPDX-License-Identifier: MIT
set -euo pipefail

# Portable helpers
die() { echo "[ERROR] $*" >&2; exit 1; }
info() { echo "[INFO]  $*"; }

# Defaults (can be overridden via environment)
: "${XG2G_DATA:=./data}"
: "${XG2G_OWI_BASE:=http://127.0.0.1}"
: "${XG2G_BOUQUET:=Premium}"
: "${XG2G_XMLTV:=xmltv.xml}"

: "${XG2G_EPG_ENABLED:=true}"
: "${XG2G_EPG_DAYS:=7}"
: "${XG2G_EPG_MAX_CONCURRENCY:=6}"
: "${XG2G_EPG_TIMEOUT_MS:=20000}"
: "${XG2G_EPG_RETRIES:=2}"

# Thresholds for sanity checks
: "${EPG_MIN_BYTES:=5242880}"          # 5 MB default
: "${EPG_MIN_PROGRAMMES:=5000}"        # 5k programmes default

# Servers
: "${XG2G_LISTEN:=:8080}"
: "${XG2G_METRICS_LISTEN:=:9090}"

# Extract port from listen address (":8080", "127.0.0.1:8080", or "8080")
extract_port() {
  local addr="$1"
  # :8080
  if [[ "$addr" =~ ^:([0-9]+)$ ]]; then
    echo "${BASH_REMATCH[1]}"; return 0
  fi
  # 127.0.0.1:8080 or [::1]:8080
  if [[ "$addr" =~ :([0-9]+)$ ]]; then
    echo "${BASH_REMATCH[1]}"; return 0
  fi
  # 8080
  if [[ "$addr" =~ ^[0-9]+$ ]]; then
    echo "$addr"; return 0
  fi
  echo ""; return 1
}

_port_main=$(extract_port "${XG2G_LISTEN}") || true
[[ -z "${_port_main}" ]] && _port_main=8080
_port_metrics=$(extract_port "${XG2G_METRICS_LISTEN}") || true
[[ -z "${_port_metrics}" ]] && _port_metrics=9090

HEALTH_URL="http://localhost:${_port_main}/healthz"
READY_URL="http://localhost:${_port_main}/readyz"
REFRESH_URL="http://localhost:${_port_main}/api/refresh"
METRICS_URL="http://localhost:${_port_metrics}/metrics"

# Generate API token if not provided
ensure_token() {
  if [[ -z "${XG2G_API_TOKEN:-}" ]]; then
    if command -v openssl >/dev/null 2>&1; then
      local rnd
      rnd=$(openssl rand -hex 16)
      export XG2G_API_TOKEN="xg2g_${rnd}"
    elif command -v uuidgen >/dev/null 2>&1; then
      local u
      u=$(uuidgen | tr 'A-Z' 'a-z' | tr -d '-')
      export XG2G_API_TOKEN="xg2g_${u}"
    else
      local ts
      ts=$(date +%s)
      export XG2G_API_TOKEN="xg2g_${RANDOM}_${ts}"
    fi
    info "Generated XG2G_API_TOKEN (remember to rotate in production)."
  fi
}

free_port() {
  local port="$1"
  # macOS/Linux portable: avoid xargs -r
  local pids
  if pids=$(lsof -ti:"${port}" 2>/dev/null); then
    if [[ -n "$pids" ]]; then
      info "Killing processes on port ${port}: ${pids}"
      # shellcheck disable=SC2086
      kill ${pids} || true
      # Give it a moment
      sleep 0.5
    fi
  fi
}

wait_http_200() {
  local url="$1"; local timeout="${2:-20}"
  local start
  start=$(date +%s)
  while true; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.3
  local now
  now=$(date +%s)
    if (( now - start > timeout )); then
      return 1
    fi
  done
}

start_daemon() {
  info "Starting daemon (XG2G_LISTEN=${XG2G_LISTEN}, metrics=${XG2G_METRICS_LISTEN})"
  # Ensure data dir exists
  mkdir -p "${XG2G_DATA}"

  # Start in background with our env
  (
    export XG2G_DATA XG2G_OWI_BASE XG2G_BOUQUET XG2G_XMLTV \
           XG2G_EPG_ENABLED XG2G_EPG_DAYS XG2G_EPG_MAX_CONCURRENCY \
           XG2G_EPG_TIMEOUT_MS XG2G_EPG_RETRIES XG2G_LISTEN \
           XG2G_METRICS_LISTEN XG2G_API_TOKEN
    go run ./cmd/daemon
  ) >/tmp/xg2g-daemon.log 2>&1 &
  DAEMON_PID=$!
  info "Daemon PID: ${DAEMON_PID} (logs: /tmp/xg2g-daemon.log)"

  # Wait for health and readiness
  wait_http_200 "$HEALTH_URL" 20 || die "healthz did not become ready"
  wait_http_200 "$READY_URL" 20 || info "readyz not ready yet (continuing)"
}

trigger_refresh() {
  info "Triggering refresh via API"
  curl -fsS -X POST "$REFRESH_URL" -H "X-API-Token: ${XG2G_API_TOKEN}" >/dev/null || die "refresh failed"
}

report_xmltv() {
  local xmlpath="${XG2G_DATA%/}/${XG2G_XMLTV}"
  info "Waiting for XMLTV at ${xmlpath}"

  local start
  start=$(date +%s)
  while [[ ! -f "$xmlpath" ]]; do
    sleep 0.5
  local now
  now=$(date +%s)
    if (( now - start > 60 )); then
      die "xmltv file not found after 60s: ${xmlpath}"
    fi
  done

  local bytes programmes
  bytes=$(wc -c < "$xmlpath" | tr -d ' ')
  programmes=$(grep -c '<programme' "$xmlpath" || true)

  echo ""
  echo "===== XMLTV Report ====="
  echo "File:    $xmlpath"
  echo "Size:    ${bytes} bytes"
  echo "Programmes: ${programmes}"
  echo "Sample:"
  grep -A3 '<programme' "$xmlpath" | head -20 || true
  echo "========================="

  # Threshold enforcement
  local fail=0
  if (( bytes < EPG_MIN_BYTES )); then
    echo "[WARN] XMLTV size (${bytes} B) below threshold (${EPG_MIN_BYTES} B)" >&2
    fail=1
  fi
  if (( programmes < EPG_MIN_PROGRAMMES )); then
    echo "[WARN] Programmes (${programmes}) below threshold (${EPG_MIN_PROGRAMMES})" >&2
    fail=1
  fi
  if (( fail == 1 )); then
    echo "[ERROR] XMLTV did not meet thresholds (size/programmes)." >&2
    return 2
  fi
}

report_metrics() {
  if curl -fsS "$METRICS_URL" >/dev/null 2>&1; then
    echo ""
    echo "===== Metrics (filtered) ====="
    curl -fsS "$METRICS_URL" | grep -E 'xg2g_(epg|xmltv)_(programmes|channels|duration)' || true
    echo "==============================="
  else
    info "metrics endpoint not reachable at ${METRICS_URL} (skipping)"
  fi
}

# Optional: direct EPG endpoint smoke-check for a single sRef using endTime
# Set SREF_SAMPLE to run (e.g., export SREF_SAMPLE="1:0:19:132F:3EF:1:C00000:0:0:0:")
direct_epg_check() {
  [[ -z "${SREF_SAMPLE:-}" ]] && return 0

  # OS-specific endTime determination
  local end_ts
  if [[ "$(uname -s)" == "Darwin" ]]; then
    end_ts=$(date -v+${XG2G_EPG_DAYS}d +%s)
  else
    end_ts=$(date -d "+${XG2G_EPG_DAYS} days" +%s)
  fi

  local url_api
  url_api="${XG2G_OWI_BASE%/}/api/epgservice?sRef=$(python3 - <<'PY'
import urllib.parse,os
print(urllib.parse.quote(os.environ['SREF_SAMPLE'], safe=''))
PY
)&time=-1&endTime=${end_ts}"

  echo ""
  echo "===== Direct EPG sample (${XG2G_EPG_DAYS}d) ====="
  echo "GET ${url_api}"
  # only show count to avoid huge output
  local cnt
  cnt=$(curl -fsS "$url_api" | grep -c 'title\|e2eventtitle' || true)
  echo "Events: ${cnt}"
  echo "===================================="
}

main() {
  ensure_token
  # Free ports idempotently
  free_port "${XG2G_LISTEN#:}"
  free_port "${XG2G_METRICS_LISTEN#:}"

  start_daemon
  # Always try to stop daemon on exit
  trap '[[ -n "${DAEMON_PID:-}" ]] && kill ${DAEMON_PID} >/dev/null 2>&1 || true' EXIT

  trigger_refresh
  report_xmltv
  report_metrics
  direct_epg_check || true
}

main "$@"
