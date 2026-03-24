#!/usr/bin/env bash
set -euo pipefail

PROJECT="xg2g"
SERVICE="xg2g"
ROOT="/srv/xg2g"
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

echo "OK: Compose contract holds (env_file + volume + no interpolation)."
