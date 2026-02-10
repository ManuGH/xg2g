#!/usr/bin/env bash
set -euo pipefail

PROJECT="xg2g"
SERVICE="xg2g"
ROOT="/srv/xg2g"
COMPOSE_FILE="$ROOT/docker-compose.yml"

cd "$ROOT"

if [ ! -f "$COMPOSE_FILE" ]; then
  echo "ERROR: compose file not found: $COMPOSE_FILE" >&2
  exit 1
fi

if grep -q '\${' "$COMPOSE_FILE"; then
  echo "ERROR: Prod docker-compose.yml contains \${...} interpolation (forbidden)." >&2
  exit 1
fi

if grep -qE '^[[:space:]]*build:[[:space:]]*' "$COMPOSE_FILE"; then
  echo "ERROR: Prod docker-compose.yml contains build: (forbidden; must be image-only)." >&2
  exit 1
fi

cfg="$(mktemp)"
trap 'rm -f "$cfg"' EXIT

docker compose --project-name "$PROJECT" config > "$cfg"

read -r env_ok vol_ok < <(awk -v svc="$SERVICE" '
function indent(line) { match(line, /^[[:space:]]*/); return RLENGTH }
{
  if ($0 ~ /^[[:space:]]*#/ || $0 ~ /^[[:space:]]*$/) next
  ind = indent($0)
  text = substr($0, ind + 1)
  if (ind == 0) {
    in_services = (text == "services:")
    in_service = 0
    in_env = 0
    in_vol = 0
    next
  }
  if (in_services && ind == 2 && text ~ /^[^[:space:]]+:/) {
    in_service = (text == svc ":")
    in_env = 0
    in_vol = 0
    next
  }
  if (!in_service) next
  if (ind == 4 && text == "env_file:") {
    in_env = 1
    in_vol = 0
    next
  }
  if (ind == 4 && text == "volumes:") {
    in_vol = 1
    in_env = 0
    next
  }
  if (ind == 4 && text ~ /^[^[:space:]]+:/) {
    in_env = 0
    in_vol = 0
    next
  }
  if (ind >= 6 && text ~ /^-[[:space:]]*/) {
    item = text
    sub(/^-+[[:space:]]*/, "", item)
    if (in_env && item == "/etc/xg2g/xg2g.env") env_ok = 1
    if (in_vol && item ~ "^/var/lib/xg2g:/var/lib/xg2g($|[[:space:]])") vol_ok = 1
  }
}
END { printf "%d %d\n", env_ok ? 1 : 0, vol_ok ? 1 : 0 }
' "$cfg")

if [ "$env_ok" != "1" ]; then
  echo "ERROR: Compose contract violated: env_file must include /etc/xg2g/xg2g.env" >&2
  exit 1
fi

if [ "$vol_ok" != "1" ]; then
  echo "ERROR: Compose contract violated: volumes must include /var/lib/xg2g:/var/lib/xg2g" >&2
  exit 1
fi

echo "OK: Compose contract holds (env_file + volume + no interpolation)."
