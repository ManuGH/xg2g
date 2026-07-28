#!/usr/bin/env bash
set -euo pipefail

ROOT=${REPO_ROOT:-"$(pwd)"}
cd "$ROOT"

ALLOWLIST=${PANIC_ALLOWLIST:-"$ROOT/panic_allowlist.txt"}
[[ -f "$ALLOWLIST" ]] || {
  echo "❌ Missing panic allowlist: $ALLOWLIST"
  exit 1
}

allowlist=$(grep -v -E '^[[:space:]]*(#|$)' "$ALLOWLIST" || true)
duplicates=$(printf '%s\n' "$allowlist" | sort | uniq -d)
if [[ -n "$duplicates" ]]; then
  echo "❌ Duplicate panic allowlist entries:"
  echo "$duplicates"
  exit 1
fi

matches=$(rg -n --glob '*.go' --glob '!**/*_test.go' 'panic\(' internal || true)
sources=""
unreviewed=""
while IFS= read -r match; do
  [[ -n "$match" ]] || continue
  source=${match#*:}
  source=${source#*:}
  source=$(printf '%s' "$source" | sed -E 's/^[[:space:]]+//')
  sources+="$source"$'\n'
  if ! printf '%s\n' "$allowlist" | grep -Fqx -- "$source"; then
    unreviewed+="$match"$'\n'
  fi
done <<< "$matches"

if [[ -n "$unreviewed" ]]; then
  echo "❌ Unreviewed production panic found:"
  printf '%s' "$unreviewed"
  exit 1
fi

stale_or_broad=""
while IFS= read -r allowed; do
  [[ -n "$allowed" ]] || continue
  count=$(printf '%s' "$sources" | grep -Fxc -- "$allowed" || true)
  if [[ "$count" -ne 1 ]]; then
    stale_or_broad+="$allowed (matches: $count; expected: 1)"$'\n'
  fi
done <<< "$allowlist"

if [[ -n "$stale_or_broad" ]]; then
  echo "❌ Panic allowlist contains stale or non-unique entries:"
  printf '%s' "$stale_or_broad"
  exit 1
fi

echo "✅ No unreviewed production panics; every exception is exact and still present"
