#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: cleanup-workspace.sh [--apply] [--aggressive]

Dry-run by default. With --apply, removes safe local workspace clutter:
- clean git worktrees under .worktrees/ and /tmp/xg2g-*
- stale /tmp/xg2g-* directories (with --aggressive)
- known local root log artifacts

Options:
  --apply       Execute cleanup actions (default is preview only)
  --aggressive  Also remove unregistered /tmp/xg2g-* directories
  -h, --help    Show this help
EOF
}

APPLY=0
AGGRESSIVE=0

for arg in "$@"; do
  case "$arg" in
    --apply) APPLY=1 ;;
    --aggressive) AGGRESSIVE=1 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $arg" >&2; usage; exit 2 ;;
  esac
done

ROOT="$(git rev-parse --show-toplevel)"
CURRENT="$(pwd -P)"

echo "repo_root=$ROOT"
echo "current_dir=$CURRENT"
echo "mode=$([[ "$APPLY" -eq 1 ]] && echo apply || echo dry-run)"
echo "aggressive=$([[ "$AGGRESSIVE" -eq 1 ]] && echo true || echo false)"
echo

declare -a WORKTREE_PATHS=()
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  WORKTREE_PATHS+=("$line")
done < <(git -C "$ROOT" worktree list --porcelain | awk 'substr($0,1,9)=="worktree "{print substr($0,10)}')

is_registered_worktree() {
  local path="$1"
  local wt
  for wt in "${WORKTREE_PATHS[@]}"; do
    if [[ "$wt" == "$path" ]]; then
      return 0
    fi
  done
  return 1
}

is_clean_worktree() {
  local path="$1"
  git -C "$path" diff --quiet &&
  git -C "$path" diff --cached --quiet &&
  [[ -z "$(git -C "$path" status --porcelain --untracked-files=normal)" ]]
}

act() {
  local msg="$1"
  shift
  if [[ "$APPLY" -eq 1 ]]; then
    echo "APPLY: $msg"
    "$@"
  else
    echo "DRYRUN: $msg"
  fi
}

maybe_remove_worktree() {
  local path="$1"
  if [[ "$path" == "$ROOT" || "$path" == "$CURRENT" ]]; then
    echo "SKIP: keep active worktree $path"
    return 0
  fi
  if [[ ! -d "$path" ]]; then
    echo "SKIP: worktree path missing $path"
    return 0
  fi
  if ! is_clean_worktree "$path"; then
    echo "SKIP: dirty worktree $path"
    return 0
  fi
  act "git worktree remove $path" git -C "$ROOT" worktree remove "$path"
}

echo "== Candidate clean worktrees =="
for wt in "${WORKTREE_PATHS[@]}"; do
  if [[ "$wt" == "$ROOT/.worktrees/"* || "$wt" == /tmp/xg2g-* ]]; then
    maybe_remove_worktree "$wt"
  fi
done
echo

echo "== Stale /tmp xg2g directories =="
for d in /tmp/xg2g-*; do
  [[ -e "$d" ]] || continue
  [[ -d "$d" ]] || continue
  if is_registered_worktree "$d"; then
    continue
  fi
  if [[ "$AGGRESSIVE" -eq 1 ]]; then
    act "rm -rf $d" rm -rf "$d"
  else
    echo "DRYRUN: stale candidate (use --aggressive to delete): $d"
  fi
done
echo

echo "== Root log artifacts =="
for f in \
  "$ROOT/build.log" \
  "$ROOT/curl.log" \
  "$ROOT/traceback.log" \
  "$ROOT/test_hang.log" \
  "$ROOT/v3_test_output.log"; do
  [[ -f "$f" ]] || continue
  act "rm -f $f" rm -f "$f"
done
for f in "$ROOT"/full_v3_test*.log; do
  [[ -f "$f" ]] || continue
  act "rm -f $f" rm -f "$f"
done
echo

echo "== Prune worktree metadata =="
act "git worktree prune" git -C "$ROOT" worktree prune

if [[ "$APPLY" -eq 1 ]]; then
  echo "Cleanup complete."
else
  echo "Preview complete. Re-run with --apply to execute."
fi

