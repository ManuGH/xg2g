#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-.}"

fail() { echo "ERROR: $*" >&2; exit 1; }

cd "$ROOT"

# 1) exactly one .git directory (this repo)
gitdirs="$(find . -name .git -type d -prune 2>/dev/null | sed 's|^\./||' | sort)"
count="$(printf "%s\n" "$gitdirs" | sed '/^$/d' | wc -l | tr -d ' ')"

if [[ "$count" -ne 1 ]]; then
  echo "Found .git directories:" >&2
  printf "%s\n" "$gitdirs" >&2
  fail "Repo hygiene violation: expected exactly 1 .git directory, found $count."
fi

# 2) forbid common drift copies inside repo
# Keep this list tight; false positives waste time.
bad_dirs=(
  "xg2g-main-21"
  "*-main-*"
  "*_backup*"
  "*-backup*"
  "*copy*"
  "*-copy*"
  "*_old*"
  "*-old*"
)

for pat in "${bad_dirs[@]}"; do
  while IFS= read -r hit; do
    [[ -z "$hit" ]] && continue
    [[ "$hit" == *"/.git/"* ]] && continue
    fail "Repo hygiene violation: drift copy directory present: $hit (pattern: $pat)"
  done < <(find . -maxdepth 2 -type d -name "$pat" 2>/dev/null | sed 's|^\./||')
done

echo "OK: repo hygiene clean (exactly one .git, no drift copies)."
