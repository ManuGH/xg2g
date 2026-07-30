#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
COMPOSE="${REPO_ROOT}/compose.monitoring.yaml"
PROMETHEUS="${REPO_ROOT}/infra/monitoring/prometheus.yml"
ALERTS="${REPO_ROOT}/infra/monitoring/alerts-xg2g-phase5-3.yml"
ALERTMANAGER="${REPO_ROOT}/infra/monitoring/alertmanager.yml"
DATASOURCE="${REPO_ROOT}/infra/monitoring/provisioning/datasources/prometheus.yml"
DASHBOARD_PROVIDER="${REPO_ROOT}/infra/monitoring/provisioning/dashboards/xg2g.yml"
DASHBOARD="${REPO_ROOT}/infra/monitoring/grafana-dashboard.json"

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

assert_contains() {
  local file="$1"
  local needle="$2"
  grep -Fq -- "${needle}" "${file}" ||
    fail "expected '${needle}' in ${file#"${REPO_ROOT}"/}"
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  if grep -Fq -- "${needle}" "${file}"; then
    fail "unexpected '${needle}' in ${file#"${REPO_ROOT}"/}"
  fi
}

for file in \
  "${COMPOSE}" \
  "${PROMETHEUS}" \
  "${ALERTS}" \
  "${ALERTMANAGER}" \
  "${DATASOURCE}" \
  "${DASHBOARD_PROVIDER}" \
  "${DASHBOARD}"; do
  [[ -s "${file}" ]] || fail "missing or empty ${file#"${REPO_ROOT}"/}"
done

assert_not_contains "${COMPOSE}" ":latest"
assert_not_contains "${COMPOSE}" "GF_SECURITY_ADMIN_PASSWORD=admin"
assert_contains "${COMPOSE}" "127.0.0.1:9091:9090"
assert_contains "${COMPOSE}" "127.0.0.1:9093:9093"
assert_contains "${COMPOSE}" "127.0.0.1:3000:3000"
assert_contains "${COMPOSE}" "XG2G_METRICS_LISTEN=:9091"
assert_contains "${COMPOSE}" "./infra/monitoring/alerts-xg2g-phase5-3.yml:/etc/prometheus/alerts-xg2g-phase5-3.yml:ro"
assert_contains "${COMPOSE}" "GF_SECURITY_ADMIN_PASSWORD__FILE=/run/secrets/grafana_admin_password"
assert_contains "${COMPOSE}" "grafana_admin_password:"
assert_contains "${COMPOSE}" "no-new-privileges:true"

assert_contains "${PROMETHEUS}" 'targets: ["xg2g:9091"]'
assert_contains "${PROMETHEUS}" 'targets: ["alertmanager:9093"]'
assert_contains "${ALERTS}" "XG2GPlaybackErrorBudgetFastBurn"
assert_contains "${ALERTS}" "XG2GPlaybackErrorBudgetSlowBurn"
assert_contains "${ALERTS}" "XG2GLiveTTFFHigh"
assert_contains "${ALERTS}" "XG2GRecordingTTFFHigh"
assert_contains "${ALERTS}" "XG2GMajorRebufferObserved"
assert_contains "${ALERTMANAGER}" "receiver: local-ui"
assert_contains "${DATASOURCE}" "uid: xg2g-prometheus"
assert_contains "${DASHBOARD_PROVIDER}" "path: /var/lib/grafana/dashboards"
assert_contains "${DASHBOARD}" "xg2g_active_ffmpeg_processes"
assert_not_contains "${DASHBOARD}" "xg2g_v3_ffmpeg_processes"

python3 - "${DASHBOARD}" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, encoding="utf-8") as handle:
    dashboard = json.load(handle)

if "dashboard" in dashboard:
    raise SystemExit("dashboard must be a file-provisioning model, not an API import envelope")
if dashboard.get("uid") != "xg2g-streaming-slos":
    raise SystemExit("dashboard UID contract is missing")
panels = dashboard.get("panels")
if not isinstance(panels, list) or len(panels) < 6:
    raise SystemExit("dashboard must contain the required SLO panels")
for panel in panels:
    datasource = panel.get("datasource", {})
    if datasource.get("uid") != "xg2g-prometheus":
        raise SystemExit(f"panel {panel.get('id')} does not use the provisioned datasource")
PY

if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  docker compose \
    --project-directory "${REPO_ROOT}" \
    -f "${REPO_ROOT}/infra/systemd/docker-compose.yml" \
    -f "${COMPOSE}" \
    config --no-env-resolution --no-path-resolution --quiet
fi

printf 'OK: monitoring compose, provisioning, and SLO alert contracts hold.\n'
