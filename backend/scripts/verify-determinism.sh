#!/usr/bin/env bash
set -euo pipefail

ROOT=${REPO_ROOT:-"$(pwd)"}
cd "$ROOT"
SCAN_DIRS_DEFAULT="internal/domain/session/manager internal/control internal/engine"
SCAN_DIRS=${DETERMINISM_SCAN_DIRS:-"$SCAN_DIRS_DEFAULT"}
ALLOWLIST=${DETERMINISM_ALLOWLIST:-"$ROOT/determinism_allowlist.txt"}

PATTERN='time\.Sleep\(|\bEventually\(|time\.After\('

matches=""
for dir in $SCAN_DIRS; do
  if [ -d "$dir" ]; then
    out=$(rg -n --glob '*_test.go' "$PATTERN" "$dir" || true)
    if [ -n "$out" ]; then
      matches+="$out"$'\n'
    fi
  fi
done

[[ -f "$ALLOWLIST" ]] || {
  echo "❌ missing determinism allowlist: $ALLOWLIST"
  exit 1
}

allowlist=$(grep -v -E '^[[:space:]]*(#|$)' "$ALLOWLIST" || true)
invalid=$(printf '%s\n' "$allowlist" | awk -F '\t' 'NF != 2 || $2 !~ /^[1-9][0-9]*$/ { print }')
if [[ -n "$invalid" ]]; then
  echo "❌ determinism allowlist entries must be: path<TAB>positive-count"
  echo "$invalid"
  exit 1
fi

duplicates=$(printf '%s\n' "$allowlist" | cut -f1 | sort | uniq -d)
if [[ -n "$duplicates" ]]; then
  echo "❌ duplicate determinism allowlist paths:"
  echo "$duplicates"
  exit 1
fi

unreviewed=""
while IFS= read -r match; do
  [[ -n "$match" ]] || continue
  path=${match%%:*}
  if ! printf '%s\n' "$allowlist" | cut -f1 | grep -Fqx -- "$path"; then
    unreviewed+="$match"$'\n'
  fi
done <<< "$matches"

if [[ -n "$unreviewed" ]]; then
  echo "❌ determinism gate found timing primitives in an unreviewed test:"
  printf '%s' "$unreviewed"
  exit 1
fi

count_drift=""
while IFS=$'\t' read -r path expected; do
  [[ -n "$path" ]] || continue
  actual=$(printf '%s' "$matches" | awk -F ':' -v expected_path="$path" '$1 == expected_path { count++ } END { print count + 0 }')
  if [[ "$actual" -ne "$expected" ]]; then
    count_drift+="$path (found: $actual; expected: $expected)"$'\n'
  fi
done <<< "$allowlist"

if [[ -n "$count_drift" ]]; then
  echo "❌ determinism allowlist drifted; review every added or removed timing primitive:"
  printf '%s' "$count_drift"
  exit 1
fi

echo "✅ determinism gate passed; timing exceptions are path-scoped and count-locked"
