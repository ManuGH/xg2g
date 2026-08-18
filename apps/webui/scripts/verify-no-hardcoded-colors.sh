#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# CTO Contract: colour is declared once, in the tokens at the top of
# src/index.css, and every other file references those tokens.
#
# This gate used to look for '#rrggbb' alone, which is why it reported green
# while whole modules were styled in rgba(). It now covers the functional
# notations too, and because that surfaces a body of pre-existing violations,
# it holds them in a baseline instead of exempting the directories they live
# in. A directory exemption is permanent in practice; a per-file count can
# only be met or lowered.
#
# Rules:
#   - a file with no baseline entry must have no hardcoded colour at all
#   - a file with a baseline entry must not exceed its recorded count
#   - a file below its recorded count is reported so the baseline can be
#     tightened with --update, which is how the debt actually shrinks
#
# rgba(var(--token)) is deliberately not matched: the pattern requires a digit
# after the opening parenthesis, so referencing a token stays legal.

BASELINE="scripts/hardcoded-colors-baseline.txt"
PATTERN='#[0-9a-fA-F]{3,8}|rgba?\([[:space:]]*[0-9]|hsla?\([[:space:]]*[0-9]'

# Counts colour values, not lines carrying one. These files put whole style
# objects on a single line, so a line-based count let someone add a second and
# third literal to an existing line without moving the number - a hole in the
# ratchet exactly where the offending code is densest.
scan() {
  while IFS= read -r file; do
    n="$(grep -oE "$PATTERN" "$file" | wc -l | tr -d ' ')"
    [ "$n" -gt 0 ] && printf '%s %s\n' "$file" "$n"
  done < <(
    grep -rlE "$PATTERN" src \
      --include='*.css' --include='*.ts' --include='*.tsx' 2>/dev/null \
      | grep -v '^src/index\.css$'
  ) | sort
}

CURRENT="$(scan || true)"

if [ "${1:-}" = "--update" ]; then
  {
    echo "# Files carrying hardcoded colours, with the number of colour values."
    echo "# Regenerate with: npm run design:colors:baseline"
    echo "# Entries may only shrink. A new file here needs a reason in review."
    printf '%s\n' "$CURRENT"
  } > "$BASELINE"
  echo "✅ Baseline written to $BASELINE."
  exit 0
fi

baseline_for() {
  [ -f "$BASELINE" ] || { echo 0; return; }
  awk -v p="$1" '!/^#/ && $1 == p { print $2; found = 1 } END { if (!found) print 0 }' "$BASELINE"
}

FAILED=0
IMPROVED=""

while read -r path count; do
  [ -n "$path" ] || continue
  allowed="$(baseline_for "$path")"
  if [ "$count" -gt "$allowed" ]; then
    echo "❌ $path: $count hardcoded colour value(s), baseline allows $allowed."
    grep -nE "$PATTERN" "$path" | head -5
    FAILED=1
  elif [ "$count" -lt "$allowed" ]; then
    IMPROVED="$IMPROVED  $path: $count now, baseline says $allowed"$'\n'
  fi
done <<< "$CURRENT"

# A file that dropped out of the scan entirely is progress the baseline has
# not caught up with yet.
if [ -f "$BASELINE" ]; then
  while read -r path count; do
    case "$path" in ''|'#'*) continue ;; esac
    if ! printf '%s\n' "$CURRENT" | grep -q "^$path "; then
      IMPROVED="$IMPROVED  $path: clean now, baseline says $count"$'\n'
    fi
  done < "$BASELINE"
fi

if [ "$FAILED" -ne 0 ]; then
  echo "   Use the tokens in src/index.css. If a value genuinely has no token yet, add one."
  exit 1
fi

if [ -n "$IMPROVED" ]; then
  echo "ℹ️  Hardcoded colours below baseline:"
  printf '%s' "$IMPROVED"
  echo "   Tighten it with: npm run design:colors:baseline"
fi

echo "✅ No hardcoded colours beyond the recorded baseline."
