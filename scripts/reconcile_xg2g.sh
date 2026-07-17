#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_RECONCILE_HOST:-root@10.10.55.2}"
REMOTE_SOURCE_ROOT="${XG2G_RECONCILE_SOURCE_ROOT:-/root/xg2g}"
REMOTE_BUILD_ROOT="${XG2G_RECONCILE_BUILD_ROOT:-/root/xg2g-build}"
CTID="${XG2G_RECONCILE_CTID:-110}"

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'USAGE'
Usage:
  scripts/reconcile_xg2g.sh status
  scripts/reconcile_xg2g.sh sync-build [--commit SHA]

status      Compare Mac, GitHub, Proxmox build, and staging evidence.
sync-build  Update only the clean Proxmox build checkout to a pushed commit.

The command never resets dirty checkouts and never deploys production.
USAGE
}

local_branch() {
  git -C "$ROOT" branch --show-current
}

local_commit() {
  git -C "$ROOT" rev-parse HEAD
}

local_dirty_count() {
  git -C "$ROOT" status --porcelain=v1 -uall | wc -l | tr -d ' '
}

status() {
  local branch commit dirty upstream origin_commit
  branch="$(local_branch)"
  commit="$(local_commit)"
  dirty="$(local_dirty_count)"
  upstream="$(git -C "$ROOT" rev-parse --abbrev-ref --symbolic-full-name '@{upstream}' 2>/dev/null || true)"
  origin_commit='unavailable'
  if [[ -n "$branch" ]]; then
    git -C "$ROOT" fetch origin "$branch" --quiet 2>/dev/null || true
    origin_commit="$(git -C "$ROOT" show-ref --verify --hash "refs/remotes/origin/$branch" 2>/dev/null || echo unavailable)"
  fi

  printf 'mac.branch=%s\n' "${branch:-detached}"
  printf 'mac.commit=%s\n' "$commit"
  printf 'mac.dirty_entries=%s\n' "$dirty"
  printf 'mac.upstream=%s\n' "${upstream:-none}"
  printf 'github.branch_commit=%s\n' "$origin_commit"
  if [[ "$origin_commit" == "$commit" ]]; then
    printf 'mac.github_relation=equal\n'
  elif [[ "$origin_commit" == unavailable ]]; then
    printf 'mac.github_relation=unknown\n'
  else
    printf 'mac.github_relation=diverged_or_unpushed\n'
  fi

  if ! ssh "$REMOTE_HOST" bash -s -- "$REMOTE_SOURCE_ROOT" "$REMOTE_BUILD_ROOT" "$CTID" <<'REMOTE'
set -u
source_root="$1"
build_root="$2"
ctid="$3"

git_state() {
  label="$1"
  path="$2"
  if [[ ! -d "$path/.git" ]]; then
    printf '%s.present=false\n' "$label"
    return 0
  fi
  printf '%s.present=true\n' "$label"
  printf '%s.branch=%s\n' "$label" "$(git -C "$path" branch --show-current)"
  printf '%s.commit=%s\n' "$label" "$(git -C "$path" rev-parse HEAD)"
  printf '%s.dirty_entries=%s\n' "$label" "$(git -C "$path" status --porcelain=v1 -uall | wc -l | tr -d ' ')"
}

git_state proxmox_source "$source_root"
git_state proxmox_build "$build_root"

if pct status "$ctid" >/dev/null 2>&1; then
  printf 'staging.present=true\n'
  pct exec "$ctid" -- /bin/sh -c '
    if [ -f /srv/xg2g-staging/deploy-manifest ]; then
      printf "staging.manifest="
      tr "\n" " " < /srv/xg2g-staging/deploy-manifest
      printf "\n"
    else
      printf "staging.manifest=missing\n"
    fi
    printf "staging.health="
    curl -fsS --max-time 3 http://127.0.0.1:8089/healthz >/dev/null 2>&1 && printf "healthy\n" || printf "unhealthy_or_unavailable\n"
    printf "staging.binary_sha256="
    docker exec xg2g-staging sha256sum /usr/local/bin/xg2g 2>/dev/null | awk "{print \$1}" || printf "unavailable"
    printf "\n"
  ' || printf 'staging.probe=failed\n'
else
  printf 'staging.present=false\n'
fi
REMOTE
  then
    printf 'remote.probe=failed\n'
  fi
}

sync_build() {
  local requested_commit branch commit origin_commit
  requested_commit=''
  if [[ "${1:-}" == '--commit' ]]; then
    [[ -n "${2:-}" ]] || die '--commit requires a SHA'
    requested_commit="$2"
    shift 2
  fi
  [[ "$#" -eq 0 ]] || die "unknown sync-build arguments: $*"

  branch="$(local_branch)"
  [[ -n "$branch" ]] || die 'detached Mac HEAD cannot be synchronized'
  [[ "$(local_dirty_count)" == '0' ]] || die 'Mac checkout is dirty; commit or stash it before sync-build'
  git -C "$ROOT" fetch origin "$branch" --quiet
  commit="$(local_commit)"
  origin_commit="$(git -C "$ROOT" show-ref --verify --hash "refs/remotes/origin/$branch")"
  [[ "$commit" == "$origin_commit" ]] || die "Mac HEAD $commit is not pushed as origin/$branch ($origin_commit)"
  if [[ -n "$requested_commit" && "$requested_commit" != "$commit" && "$requested_commit" != "${commit:0:${#requested_commit}}" ]]; then
    die "requested commit $requested_commit does not match Mac HEAD $commit"
  fi

  printf 'Synchronizing Proxmox build checkout to %s (%s)...\n' "$branch" "$commit"
  ssh "$REMOTE_HOST" bash -s -- "$REMOTE_SOURCE_ROOT" "$REMOTE_BUILD_ROOT" "$branch" "$commit" <<'REMOTE'
set -euo pipefail
source_root="$1"
build_root="$2"
branch="$3"
commit="$4"

if [[ ! -d "$build_root/.git" ]]; then
  [[ ! -e "$build_root" ]] || { echo "ERROR: $build_root exists but is not a Git checkout" >&2; exit 1; }
  git clone "$(git -C "$source_root" remote get-url origin)" "$build_root"
fi

[[ -z "$(git -C "$build_root" status --porcelain=v1 -uall)" ]] || {
  echo "ERROR: Proxmox build checkout is dirty; refusing to overwrite it" >&2
  exit 1
}

git -C "$build_root" fetch origin "$branch" --quiet
[[ "$(git -C "$build_root" show-ref --verify --hash "refs/remotes/origin/$branch")" == "$commit" ]] || {
  echo "ERROR: origin/$branch does not contain requested commit $commit" >&2
  exit 1
}
git -C "$build_root" switch --detach "$commit"
[[ "$(git -C "$build_root" rev-parse HEAD)" == "$commit" ]]
printf 'proxmox_build.commit=%s\n' "$(git -C "$build_root" rev-parse HEAD)"
REMOTE
  printf 'Build checkout synchronized. No LXC deployment was performed.\n'
  printf 'Next step for staging: scripts/fast_deploy.sh\n'
}

case "${1:-status}" in
  status)
    [[ "$#" -eq 1 ]] || die 'status takes no arguments'
    status
    ;;
  sync-build)
    shift
    sync_build "$@"
    ;;
  -h|--help|help)
    usage
    ;;
  *)
    usage >&2
    exit 64
    ;;
esac
