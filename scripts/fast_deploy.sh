#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
REMOTE_HOST="${XG2G_DEPLOY_HOST:-proxmox}"
REMOTE_ROOT="${XG2G_DEPLOY_ROOT:-/root/xg2g}"

die() {
  echo "ERROR: $*" >&2
  exit 1
}

cd "${ROOT}"
branch="$(git branch --show-current)"
[[ -n "${branch}" ]] || die "detached HEAD is not deployable"
[[ "${branch}" =~ ^[A-Za-z0-9._/-]+$ ]] || die "unsafe branch name: ${branch}"
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || \
  die "tracked Mac changes must be committed before deployment"

git fetch origin "${branch}" --quiet
[[ -z "$(git rev-list "origin/${branch}..HEAD")" ]] || \
  die "push HEAD to origin/${branch} before deployment"
[[ -z "$(git rev-list "HEAD..origin/${branch}")" ]] || \
  die "local branch is behind origin/${branch}; update it before deployment"

echo "Preparing ${branch} on ${REMOTE_HOST}:${REMOTE_ROOT}..."
ssh "${REMOTE_HOST}" bash -s -- "${REMOTE_ROOT}" "${branch}" <<'REMOTE'
set -euo pipefail
root="$1"
branch="$2"

cd "${root}"
[[ -z "$(git status --porcelain --untracked-files=no)" ]] || {
  echo "ERROR: tracked Proxmox changes must be committed before deployment" >&2
  exit 1
}

git fetch origin "${branch}" --quiet
if git show-ref --verify --quiet "refs/heads/${branch}"; then
  git switch "${branch}"
else
  git switch --track -c "${branch}" "origin/${branch}"
fi
git merge --ff-only "origin/${branch}"

make build-with-ui
scripts/deploy-fast-iteration.sh
REMOTE
