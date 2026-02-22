#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-.}"

fail() { echo "ERROR: $*" >&2; exit 1; }

cd "$ROOT"

# 1) root git metadata must exist; nested .git metadata is forbidden
if [[ ! -e .git ]]; then
  fail "Repo hygiene violation: missing root .git metadata."
fi

# Prefer fd for fast recursive metadata lookup on large/slow worktrees.
# Fall back to find where fd is unavailable.
if command -v fd >/dev/null 2>&1; then
  nested_git_meta="$(
    {
      fd -HI --no-ignore -t d '^\.git$' . \
        -E .worktrees \
        -E vendor \
        -E node_modules \
        -E webui/node_modules
      fd -HI --no-ignore -t f '^\.git$' . \
        -E .worktrees \
        -E vendor \
        -E node_modules \
        -E webui/node_modules
    } 2>/dev/null \
      | sed 's|^\./||' \
      | awk '$0 != ".git"' \
      | sort -u
  )"
else
  nested_git_meta="$(
    find . \
      -maxdepth 4 \
      \( \
        -path './.git' -o \
        -path './.worktrees' -o \
        -path './vendor' -o \
        -path './node_modules' -o \
        -path './webui/node_modules' \
      \) -prune \
      -o \
      -mindepth 2 \
      \( -type d -name .git -o -type f -name .git \) \
      -print 2>/dev/null \
      | sed 's|^\./||' \
      | sort -u
  )"
fi
if [[ -n "$nested_git_meta" ]]; then
  echo "Nested .git metadata found:" >&2
  printf "%s\n" "$nested_git_meta" >&2
  fail "Repo hygiene violation: nested .git metadata is not allowed."
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

# 3) forbid committed artifact-like files (source of audit drift)
forbidden_name_re='(^|/)([^/]*\.log|[^/]*_logs\.txt|debug[^/]*\.txt|vuln[^/]*\.txt|test_output[^/]*\.txt|test_results[^/]*\.txt)$'
forbidden_hits="$(git ls-files | grep -E "$forbidden_name_re" || true)"
if [[ -n "$forbidden_hits" ]]; then
  echo "Forbidden artifact-like files are committed:" >&2
  printf '%s\n' "$forbidden_hits" >&2
  fail "Repo hygiene violation: remove transient runtime/test/security artifacts from git."
fi

# 3b) forbid committed local worktree internals
forbidden_path_re='^\.worktrees($|/)'
forbidden_path_hits="$(git ls-files | grep -E "$forbidden_path_re" || true)"
if [[ -n "$forbidden_path_hits" ]]; then
  echo "Forbidden local workspace paths are committed:" >&2
  printf '%s\n' "$forbidden_path_hits" >&2
  fail "Repo hygiene violation: .worktrees is local-only and must never be committed."
fi

# 4) fail-closed scan for runtime-sensitive patterns in tracked text artifacts
# Allowlist is intentionally narrow: only scrubbed fixtures under testdata/fixtures/**.
sensitive_re='([0-9]{1,3}\.){3}[0-9]{1,3}|request_id[[:space:]]*[:=]|session_id[[:space:]]*[:=]|correlation_id[[:space:]]*[:=]|authorization[[:space:]]*:|bearer[[:space:]]+'
sensitive_violations=0

while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  [[ "$file" == vendor/* ]] && continue
  [[ ! -f "$file" ]] && continue

  if [[ "$file" == testdata/fixtures/* ]]; then
    if grep -q "REDACTED" "$file"; then
      continue
    fi
    echo "Unscrubbed fixture contains sensitive markers: $file" >&2
    sensitive_violations=1
    continue
  fi

  echo "Sensitive marker found in tracked artifact-like file: $file" >&2
  sensitive_violations=1
done < <(
  git grep -n -I -E "$sensitive_re" -- '*.txt' '*.log' '*.jsonl' '*.ndjson' 2>/dev/null \
    | cut -d: -f1 \
    | sort -u
)

if [[ "$sensitive_violations" -ne 0 ]]; then
  fail "Repo hygiene violation: sensitive runtime markers detected outside scrubbed fixture allowlist."
fi

echo "OK: repo hygiene clean (root git metadata, no nested git metadata, no drift copies, no artifact leaks)."
