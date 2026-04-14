#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

MODE=""
SOURCE_ROOT="${REPO_ROOT}"
TARGET_ROOT="/opt/xg2g"
SOURCE_REF="HEAD"
ALLOW_SOURCE_DIRTY=0
FORCE_TARGET_DIRTY=0
BACKUP_ROOT=""

SOURCE_COMMIT=""
SNAPSHOT_ROOT=""
NEW_MANIFEST=""
OLD_MANIFEST=""
MISSING_LIST=""
CHANGED_LIST=""
STALE_LIST=""
BACKUP_DIR=""
TEMP_DIRS=()

SYNC_META_DIR=".runtime-bin/runtime-sync"
SYNC_MANIFEST_REL="${SYNC_META_DIR}/manifest.txt"
SYNC_SOURCE_REL="${SYNC_META_DIR}/source.env"

usage() {
  cat <<'EOF'
Usage:
  deploy/runtime-sync.sh --check [--source <path>] [--target <path>] [--ref <git-ref>] [--allow-source-dirty]
  deploy/runtime-sync.sh --apply [--source <path>] [--target <path>] [--ref <git-ref>] [--allow-source-dirty] [--force-target-dirty] [--backup-root <path>]

Purpose:
  Keep a separate runtime workspace (for example /opt/xg2g) aligned with one
  writable git checkout. The source checkout is the only git source of truth.

Modes:
  --check   Compare the pinned source ref against the runtime workspace.
  --apply   Backup overwritten files, copy the pinned source ref into the
            runtime workspace, then rerun --check.

Options:
  --source <path>         Writable source checkout. Defaults to the current repo.
  --target <path>         Runtime workspace. Defaults to /opt/xg2g.
  --ref <git-ref>         Source git ref to sync. Defaults to HEAD.
  --allow-source-dirty    Allow syncing from a dirty source checkout.
  --force-target-dirty    Allow overwriting a dirty git workspace at the target.
  --backup-root <path>    Where overwritten target files are backed up during
                          --apply. Defaults to <target>/.runtime-bin/runtime-sync/backups.
  --help                  Show this help.

Exit codes:
  0  runtime workspace matches the source ref
  1  drift detected
EOF
}

note() {
  printf 'INFO: %s\n' "$*" >&2
}

warn() {
  printf 'WARN: %s\n' "$*" >&2
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

cleanup() {
  local dir
  for dir in "${TEMP_DIRS[@]:-}"; do
    [[ -n "${dir}" && -d "${dir}" ]] && rm -rf "${dir}"
  done
}

trap cleanup EXIT

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "required tool not found: $1"
}

canonical_dir() {
  local path="$1"
  (cd "${path}" && pwd -P)
}

stat_mode() {
  local path="$1"
  if stat -c '%a' "${path}" >/dev/null 2>&1; then
    stat -c '%a' "${path}"
    return 0
  fi
  stat -f '%Lp' "${path}"
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --check)
        [[ -z "${MODE}" ]] || fail "choose exactly one of --check or --apply"
        MODE="check"
        shift
        ;;
      --apply)
        [[ -z "${MODE}" ]] || fail "choose exactly one of --check or --apply"
        MODE="apply"
        shift
        ;;
      --source)
        [[ "$#" -ge 2 ]] || fail "--source requires a value"
        SOURCE_ROOT="$2"
        shift 2
        ;;
      --target)
        [[ "$#" -ge 2 ]] || fail "--target requires a value"
        TARGET_ROOT="$2"
        shift 2
        ;;
      --ref)
        [[ "$#" -ge 2 ]] || fail "--ref requires a value"
        SOURCE_REF="$2"
        shift 2
        ;;
      --allow-source-dirty)
        ALLOW_SOURCE_DIRTY=1
        shift
        ;;
      --force-target-dirty)
        FORCE_TARGET_DIRTY=1
        shift
        ;;
      --backup-root)
        [[ "$#" -ge 2 ]] || fail "--backup-root requires a value"
        BACKUP_ROOT="$2"
        shift 2
        ;;
      --help|-h)
        usage
        exit 0
        ;;
      *)
        fail "unknown argument: $1"
        ;;
    esac
  done

  [[ -n "${MODE}" ]] || fail "choose one of --check or --apply"
}

ensure_source_root() {
  git -C "${SOURCE_ROOT}" rev-parse --show-toplevel >/dev/null 2>&1 || fail "source is not a git checkout: ${SOURCE_ROOT}"
  SOURCE_ROOT="$(git -C "${SOURCE_ROOT}" rev-parse --show-toplevel)"
  SOURCE_COMMIT="$(git -C "${SOURCE_ROOT}" rev-parse --verify "${SOURCE_REF}^{commit}")" || fail "unable to resolve source ref: ${SOURCE_REF}"

  if [[ "${ALLOW_SOURCE_DIRTY}" -eq 0 ]]; then
    if [[ -n "$(git -C "${SOURCE_ROOT}" status --porcelain)" ]]; then
      fail "source checkout is dirty: ${SOURCE_ROOT} (commit or pass --allow-source-dirty)"
    fi
  fi
}

ensure_target_root() {
  mkdir -p "${TARGET_ROOT}"
  TARGET_ROOT="$(canonical_dir "${TARGET_ROOT}")"

  if [[ "$(canonical_dir "${SOURCE_ROOT}")" == "${TARGET_ROOT}" ]]; then
    fail "source and target must be different directories"
  fi
}

check_target_workspace() {
  if git -C "${TARGET_ROOT}" rev-parse --show-toplevel >/dev/null 2>&1; then
    if [[ -n "$(git -C "${TARGET_ROOT}" status --porcelain)" ]]; then
      if [[ "${FORCE_TARGET_DIRTY}" -eq 0 ]]; then
        fail "target git workspace is dirty: ${TARGET_ROOT} (rerun with --force-target-dirty if this workspace is deploy-only)"
      fi
      warn "overwriting dirty target git workspace: ${TARGET_ROOT}"
    fi
  fi
}

prepare_snapshot() {
  local temp_root
  temp_root="$(mktemp -d)"
  TEMP_DIRS+=("${temp_root}")

  SNAPSHOT_ROOT="${temp_root}/snapshot"
  NEW_MANIFEST="${temp_root}/manifest.new"
  OLD_MANIFEST="${temp_root}/manifest.old"
  MISSING_LIST="${temp_root}/missing.txt"
  CHANGED_LIST="${temp_root}/changed.txt"
  STALE_LIST="${temp_root}/stale.txt"

  mkdir -p "${SNAPSHOT_ROOT}"
  git -C "${SOURCE_ROOT}" archive "${SOURCE_COMMIT}" | tar -xf - -C "${SNAPSHOT_ROOT}"
  (
    cd "${SNAPSHOT_ROOT}"
    find . -type f | sed 's#^\./##' | LC_ALL=C sort > "${NEW_MANIFEST}"
  )

  if [[ -f "${TARGET_ROOT}/${SYNC_MANIFEST_REL}" ]]; then
    LC_ALL=C sort -u "${TARGET_ROOT}/${SYNC_MANIFEST_REL}" > "${OLD_MANIFEST}"
  elif git -C "${TARGET_ROOT}" rev-parse --show-toplevel >/dev/null 2>&1; then
    git -C "${TARGET_ROOT}" ls-files | LC_ALL=C sort -u > "${OLD_MANIFEST}"
  else
    : > "${OLD_MANIFEST}"
  fi

  : > "${MISSING_LIST}"
  : > "${CHANGED_LIST}"
  : > "${STALE_LIST}"
}

record_drift() {
  local file="$1"
  local rel="$2"
  printf '%s\n' "${rel}" >> "${file}"
}

collect_drift() {
  local rel target_path

  while IFS= read -r rel; do
    [[ -n "${rel}" ]] || continue
    target_path="${TARGET_ROOT}/${rel}"

    if [[ ! -e "${target_path}" && ! -L "${target_path}" ]]; then
      record_drift "${MISSING_LIST}" "${rel}"
      continue
    fi

    if ! cmp -s "${SNAPSHOT_ROOT}/${rel}" "${target_path}"; then
      record_drift "${CHANGED_LIST}" "${rel}"
      continue
    fi

    if [[ "$(stat_mode "${SNAPSHOT_ROOT}/${rel}")" != "$(stat_mode "${target_path}")" ]]; then
      record_drift "${CHANGED_LIST}" "${rel}"
    fi
  done < "${NEW_MANIFEST}"

  if [[ -s "${OLD_MANIFEST}" ]]; then
    while IFS= read -r rel; do
      [[ -n "${rel}" ]] || continue
      target_path="${TARGET_ROOT}/${rel}"
      if [[ -e "${target_path}" || -L "${target_path}" ]]; then
        record_drift "${STALE_LIST}" "${rel}"
      fi
    done < <(comm -23 "${OLD_MANIFEST}" "${NEW_MANIFEST}")
  fi
}

count_lines() {
  local file="$1"
  if [[ -s "${file}" ]]; then
    wc -l < "${file}"
  else
    printf '0\n'
  fi
}

show_preview() {
  local label="$1"
  local file="$2"
  local total

  [[ -s "${file}" ]] || return 0

  total="$(count_lines "${file}")"
  note "${label} (${total})"
  sed -n '1,20s#^#  - #p' "${file}" >&2
  if [[ "${total}" -gt 20 ]]; then
    note "  ... plus $((total - 20)) more"
  fi
}

backup_existing_path() {
  local rel="$1"
  local target_path="${TARGET_ROOT}/${rel}"
  local backup_path

  if [[ ! -e "${target_path}" && ! -L "${target_path}" ]]; then
    return 0
  fi

  [[ -n "${BACKUP_DIR}" ]] || fail "backup directory not initialized"

  backup_path="${BACKUP_DIR}/${rel}"
  mkdir -p "$(dirname "${backup_path}")"
  cp -a "${target_path}" "${backup_path}"
}

prune_empty_dirs() {
  local rel="$1"
  local parent="${TARGET_ROOT}/$(dirname "${rel}")"

  while [[ "${parent}" != "${TARGET_ROOT}" ]]; do
    rmdir "${parent}" >/dev/null 2>&1 || break
    parent="$(dirname "${parent}")"
  done
}

write_sync_metadata() {
  mkdir -p "${TARGET_ROOT}/${SYNC_META_DIR}"
  cp "${NEW_MANIFEST}" "${TARGET_ROOT}/${SYNC_MANIFEST_REL}"
  cat > "${TARGET_ROOT}/${SYNC_SOURCE_REL}" <<EOF
source_root=${SOURCE_ROOT}
source_ref=${SOURCE_REF}
source_commit=${SOURCE_COMMIT}
synced_at=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF
}

run_check() {
  local missing_count changed_count stale_count

  collect_drift

  missing_count="$(count_lines "${MISSING_LIST}")"
  changed_count="$(count_lines "${CHANGED_LIST}")"
  stale_count="$(count_lines "${STALE_LIST}")"

  if [[ "${missing_count}" == "0" && "${changed_count}" == "0" && "${stale_count}" == "0" ]]; then
    note "runtime workspace matches ${SOURCE_COMMIT}"
    return 0
  fi

  note "runtime workspace drift detected against ${SOURCE_COMMIT}"
  show_preview "missing files" "${MISSING_LIST}"
  show_preview "changed files" "${CHANGED_LIST}"
  show_preview "stale files" "${STALE_LIST}"
  return 1
}

apply_sync() {
  local needs_backup=0 rel

  if [[ -z "${BACKUP_ROOT}" ]]; then
    BACKUP_ROOT="${TARGET_ROOT}/${SYNC_META_DIR}/backups"
  fi

  if [[ -s "${CHANGED_LIST}" || -s "${STALE_LIST}" ]]; then
    needs_backup=1
    BACKUP_DIR="${BACKUP_ROOT}/$(date -u +%Y%m%dT%H%M%SZ)"
    mkdir -p "${BACKUP_DIR}"
  fi

  if [[ -s "${CHANGED_LIST}" ]]; then
    while IFS= read -r rel; do
      [[ -n "${rel}" ]] || continue
      backup_existing_path "${rel}"
    done < "${CHANGED_LIST}"
  fi

  if [[ -s "${STALE_LIST}" ]]; then
    while IFS= read -r rel; do
      [[ -n "${rel}" ]] || continue
      backup_existing_path "${rel}"
      rm -rf "${TARGET_ROOT}/${rel}"
      prune_empty_dirs "${rel}"
    done < "${STALE_LIST}"
  fi

  tar -C "${SNAPSHOT_ROOT}" -cf - . | tar -C "${TARGET_ROOT}" -xf -
  write_sync_metadata

  if [[ "${needs_backup}" -eq 1 ]]; then
    note "backup written to ${BACKUP_DIR}"
  fi
}

main() {
  require_tool git
  require_tool tar
  require_tool cmp
  require_tool comm
  require_tool mktemp

  parse_args "$@"
  ensure_source_root
  ensure_target_root
  check_target_workspace
  prepare_snapshot

  if [[ "${MODE}" == "check" ]]; then
    run_check
    exit $?
  fi

  if run_check; then
    write_sync_metadata
    note "runtime workspace already up to date"
    exit 0
  fi

  apply_sync

  prepare_snapshot
  if run_check; then
    note "runtime workspace synced to ${SOURCE_COMMIT}"
    exit 0
  fi

  fail "runtime workspace still drifted after apply"
}

main "$@"
