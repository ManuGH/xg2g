#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# CTO Contract: the typographic identity lives in tokens, not in rules.
#
# Two ways it erodes, both caught here:
#   1. A rule sets a literal font stack instead of var(--font-*). One such
#      line and part of the UI silently falls back to the system font again,
#      which is exactly the state this replaced.
#   2. A rule adopts var(--font-label) but keeps its own letterspacing. The
#      mono labels were tracked at five different values before they were
#      unified; splitting them again is how a design language decays back
#      into a theme.
#
# Token definitions in index.css declare the literal stacks and are the one
# place allowed to name a typeface.

FAILED=0

LITERAL="$(grep -rn 'font-family:' src --include='*.css' \
  | grep -v 'var(--font' \
  | grep -v '^src/index.css:[0-9]*: *--font-' || true)"

if [ -n "$LITERAL" ]; then
  echo "$LITERAL"
  echo "❌ Literal font stack outside the token definitions."
  echo "   Use var(--font-display|heading|body|label|mono) instead."
  FAILED=1
fi

while IFS= read -r file; do
  fam="$(grep -c 'var(--font-label)' "$file" || true)"
  trk="$(grep -c 'var(--tracking-label)' "$file" || true)"
  if [ "$fam" != "$trk" ]; then
    echo "$file: var(--font-label) x$fam but var(--tracking-label) x$trk"
    echo "❌ Label face and label tracking must travel together."
    FAILED=1
  fi
done < <(grep -rl 'var(--font-label)' src --include='*.css' || true)

if [ "$FAILED" -ne 0 ]; then
  exit 1
fi

echo "✅ Typography contract OK."
