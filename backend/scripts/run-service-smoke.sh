#!/usr/bin/env bash
set -euo pipefail

PROJECT="xg2g"
SERVICE="xg2g"
UNIT="xg2g"
COMPOSE_FILE="/srv/xg2g/docker-compose.yml"
ENV_FILE="/etc/xg2g/xg2g.env"
COMPOSE_DIR="/srv/xg2g"

if [ "$(id -u)" != "0" ]; then
  echo "ERROR: must run as root (uses systemctl and /etc/xg2g/xg2g.env)" >&2
  exit 1
fi

if [ -f "${COMPOSE_FILE}.bak" ] || [ -f "${ENV_FILE}.bak" ]; then
  echo "ERROR: backup files exist (.bak). Restore them before running smoke." >&2
  exit 1
fi

cd "$COMPOSE_DIR"

cleanup_runtime() {
  systemctl stop "$UNIT" >/dev/null 2>&1 || true
  docker compose --project-name "$PROJECT" down --remove-orphans >/dev/null 2>&1 || true
  systemctl reset-failed "$UNIT" >/dev/null 2>&1 || true
}

restore_files() {
  if [ -f "${COMPOSE_FILE}.bak" ] && [ ! -f "$COMPOSE_FILE" ]; then
    mv "${COMPOSE_FILE}.bak" "$COMPOSE_FILE"
  fi
  if [ -f "${ENV_FILE}.bak" ] && [ ! -f "$ENV_FILE" ]; then
    mv "${ENV_FILE}.bak" "$ENV_FILE"
  fi
}

must_empty_container() {
  local cid
  cid="$(docker compose --project-name "$PROJECT" ps -q "$SERVICE" || true)"
  if [ -n "$cid" ]; then
    echo "ERROR: Expected no container, but got cid=$cid" >&2
    exit 1
  fi
}

must_failed_exec() {
  local result
  result="$(systemctl show -p Result --value "$UNIT" || true)"
  case "$result" in
    failed|exit-code|resources) return 0 ;;
    *) echo "ERROR: Expected Result=failed|exit-code|resources, got: $result" >&2; exit 1 ;;
  esac
}

must_failed_or_condition() {
  local result
  result="$(systemctl show -p Result --value "$UNIT" || true)"
  case "$result" in
    condition|failed|exit-code|resources) return 0 ;;
    *) echo "ERROR: Expected Result=condition|failed|exit-code|resources, got: $result" >&2; exit 1 ;;
  esac
}

trap 'restore_files' EXIT

echo "== Smoke: pre-clean =="
cleanup_runtime

echo "== 1) Missing compose file =="
mv "$COMPOSE_FILE" "${COMPOSE_FILE}.bak"
systemctl start "$UNIT" >/dev/null 2>&1 || true
must_failed_or_condition
mv "${COMPOSE_FILE}.bak" "$COMPOSE_FILE"
must_empty_container
cleanup_runtime

echo "== 2) Missing env file =="
mv "$ENV_FILE" "${ENV_FILE}.bak"
systemctl start "$UNIT" >/dev/null 2>&1 || true
must_failed_exec
mv "${ENV_FILE}.bak" "$ENV_FILE"
must_empty_container
cleanup_runtime

echo "== 3) Whitespace token =="
cp "$ENV_FILE" "${ENV_FILE}.bak"
awk -F= '$1 != "XG2G_API_TOKEN" { print } END { print "XG2G_API_TOKEN=\"   \"" }' \
  "$ENV_FILE" > "${ENV_FILE}.tmp"
mv "${ENV_FILE}.tmp" "$ENV_FILE"
systemctl start "$UNIT" >/dev/null 2>&1 || true
must_failed_exec
mv "${ENV_FILE}.bak" "$ENV_FILE"
must_empty_container
cleanup_runtime

echo "== 4) Valid start + healthy =="
systemctl start "$UNIT"
cid="$(docker compose --project-name "$PROJECT" ps -q "$SERVICE")"
test -n "$cid"
status="$(docker inspect --format '{{.State.Health.Status}}' "$cid")"
if [ "$status" != "healthy" ]; then
  echo "ERROR: expected healthy, got: $status" >&2
  docker compose --project-name "$PROJECT" logs --tail=200 >&2 || true
  exit 1
fi

echo "== 5) Reload idempotent (CID unchanged) =="
cid_before="$cid"
systemctl reload "$UNIT"
cid_after="$(docker compose --project-name "$PROJECT" ps -q "$SERVICE")"
if [ "$cid_before" != "$cid_after" ]; then
  echo "ERROR: expected no recreation; cid_before=$cid_before cid_after=$cid_after" >&2
  exit 1
fi

echo "OK: All smoke cases passed."
