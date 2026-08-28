#!/usr/bin/env bash
set -euo pipefail

PROJECT="xg2g"
SERVICE="xg2g"
ROOT="${XG2G_COMPOSE_ROOT:-/srv/xg2g}"
COMPOSE_HELPER="$ROOT/scripts/compose-xg2g.sh"

cd "$ROOT"

if [ ! -x "$COMPOSE_HELPER" ]; then
  echo "ERROR: compose helper not found or not executable: $COMPOSE_HELPER" >&2
  exit 1
fi

mapfile -t COMPOSE_FILES < <("$COMPOSE_HELPER" --print-files)

service_list_contains() {
  local key="$1"
  local wanted="$2"
  local compose_file

  for compose_file in "${COMPOSE_FILES[@]}"; do
    if awk -v svc="$SERVICE" -v key="$key" -v wanted="$wanted" '
function indent(line) { match(line, /^[[:space:]]*/); return RLENGTH }
function normalize(value) {
  sub(/^[[:space:]]+/, "", value)
  sub(/[[:space:]]+$/, "", value)
  gsub(/^["'\'']|["'\'']$/, "", value)
  return value
}
{
  if ($0 ~ /^[[:space:]]*#/ || $0 ~ /^[[:space:]]*$/) next

  ind = indent($0)
  text = substr($0, ind + 1)

  if (ind == 0) {
    in_services = (text == "services:")
    in_service = 0
    in_key = 0
    next
  }

  if (in_services && ind == 2 && text ~ /^[^[:space:]]+:[[:space:]]*$/) {
    in_service = (text == svc ":")
    in_key = 0
    next
  }

  if (!in_service) next

  if (ind == 4 && text == key ":") {
    in_key = 1
    next
  }

  if (ind == 4 && text ~ ("^" key ":[[:space:]]*[^[:space:]].*$")) {
    item = text
    sub(("^" key ":[[:space:]]*"), "", item)
    if (normalize(item) == wanted) found = 1
    in_key = 0
    next
  }

  if (ind == 4 && text ~ /^[^[:space:]]+:[[:space:]]*$/) {
    in_key = 0
    next
  }

  if (in_key && ind >= 6 && text ~ /^-[[:space:]]*/) {
    item = text
    sub(/^-+[[:space:]]*/, "", item)
    if (normalize(item) == wanted) found = 1
    next
  }

  if (in_key && ind <= 4) {
    in_key = 0
  }
}
END { exit(found ? 0 : 1) }
' "$compose_file"; then
      return 0
    fi
  done

  return 1
}

# Reads a scalar key from services.<SERVICE> in a single compose file.
# Empty output means the file does not set the key at all.
service_scalar_value() {
  local compose_file="$1"
  local key="$2"

  awk -v svc="$SERVICE" -v key="$key" '
function indent(line) { match(line, /^[[:space:]]*/); return RLENGTH }
function normalize(value) {
  sub(/^[[:space:]]+/, "", value)
  sub(/[[:space:]]+$/, "", value)
  gsub(/^["'\'']|["'\'']$/, "", value)
  return value
}
{
  if ($0 ~ /^[[:space:]]*#/ || $0 ~ /^[[:space:]]*$/) next

  ind = indent($0)
  text = substr($0, ind + 1)

  if (ind == 0) {
    in_services = (text == "services:")
    in_service = 0
    next
  }

  if (in_services && ind == 2 && text ~ /^[^[:space:]]+:[[:space:]]*$/) {
    in_service = (text == svc ":")
    next
  }

  if (!in_service) next

  if (ind == 4 && text ~ ("^" key ":[[:space:]]*[^[:space:]].*$")) {
    value = text
    sub(("^" key ":[[:space:]]*"), "", value)
    sub(/[[:space:]]+#.*$/, "", value)
    print tolower(normalize(value))
  }
}
' "$compose_file" | tail -n 1
}

for compose_file in "${COMPOSE_FILES[@]}"; do
  if grep -q '\${' "$compose_file"; then
    echo "ERROR: Compose file contains \${...} interpolation (forbidden): $compose_file" >&2
    exit 1
  fi

  if grep -qE '^[[:space:]]*build:[[:space:]]*' "$compose_file"; then
    echo "ERROR: Compose file contains build: (forbidden; must be image-only): $compose_file" >&2
    exit 1
  fi
done

if ! service_list_contains "env_file" "/etc/xg2g/xg2g.env"; then
  echo "ERROR: Compose contract violated: env_file must include /etc/xg2g/xg2g.env" >&2
  exit 1
fi

if ! service_list_contains "volumes" "/var/lib/xg2g:/var/lib/xg2g"; then
  echo "ERROR: Compose contract violated: volumes must include /var/lib/xg2g:/var/lib/xg2g" >&2
  exit 1
fi

if service_list_contains "ports" "8088:8088"; then
  echo "ERROR: Compose contract violated: backend port must not publish on every host interface" >&2
  exit 1
fi

if ! service_list_contains "ports" "127.0.0.1:8088:8088"; then
  echo "ERROR: Compose contract violated: base backend port must bind to 127.0.0.1" >&2
  exit 1
fi

# The daemon supervises child processes: the media core runs in its own process
# group, and its descendants are orphaned onto PID 1 when the core leaves. A Go
# daemon as PID 1 reaps only its own children, so without a real init/subreaper
# those descendants stay as zombies, the process group never empties, and the
# lifecycle contract in internal/stream/ingest/remotecore cannot terminate early.
#
# The first file is the base by construction - docker compose merges the later
# ones on top of it - so the base is what has to carry the invariant, and no
# selected overlay may take it away again.
base_compose="${COMPOSE_FILES[0]}"
base_init="$(service_scalar_value "$base_compose" "init")"
if [ "$base_init" != "true" ]; then
  echo "ERROR: Compose contract violated: base compose must set services.$SERVICE.init: true (found: ${base_init:-<unset>} in $base_compose)" >&2
  exit 1
fi

for compose_file in "${COMPOSE_FILES[@]}"; do
  overlay_init="$(service_scalar_value "$compose_file" "init")"
  if [ -n "$overlay_init" ] && [ "$overlay_init" != "true" ]; then
    echo "ERROR: Compose contract violated: services.$SERVICE.init must stay true; $compose_file sets it to $overlay_init" >&2
    exit 1
  fi
done

echo "OK: Compose contract holds (loopback port + env_file + volume + init + no interpolation)."
