#!/usr/bin/env bash
set -euo pipefail

INSTALL_ROOT="/"
ENV_PATH="/etc/xg2g/xg2g.env"
DATA_PATH="/var/lib/xg2g"
BACKUP_PATH="/var/backups/xg2g"
SERVICE_NAME="xg2g.service"

usage() {
  cat <<'EOF'
Usage:
  sudo xg2g-admin doctor [--install-root DIR]
  sudo xg2g-admin backup [--install-root DIR]
  sudo xg2g-admin restore ARCHIVE --yes [--install-root DIR]
  sudo xg2g-admin update --ref TAG_OR_SHA [--install-root DIR]
  sudo xg2g-admin rollback --yes [--install-root DIR]
  sudo xg2g-admin uninstall [--purge-data --yes] [--install-root DIR]

Commands:
  doctor     Check prerequisites, permissions, storage, container health, and HTTPS.
  backup     Create an atomic backup of durable SQLite/JSON state and configuration.
  restore    Validate and restore a backup after creating a safety backup.
  update     Back up, install a pinned ref, health-check it, and roll back on failure.
  rollback   Reinstall the ref recorded immediately before the last update.
  uninstall  Remove services and deployed runtime files. Data is preserved by default.

The command never partitions, formats, mounts, or deletes an external DVR path.
EOF
}

info() {
  printf 'INFO: %s\n' "$*"
}

warn() {
  printf 'WARN: %s\n' "$*" >&2
}

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

host_path() {
  local path="$1"
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    printf '%s\n' "${path}"
  else
    printf '%s%s\n' "${INSTALL_ROOT%/}" "${path}"
  fi
}

require_root() {
  if [[ "${INSTALL_ROOT}" == "/" && "${EUID}" -ne 0 ]]; then
    fail "run this host operation with sudo"
  fi
}

env_value() {
  local key="$1"
  local env_file
  env_file="$(host_path "${ENV_PATH}")"
  [[ -f "${env_file}" ]] || return 1
  python3 - "${env_file}" "${key}" <<'PY'
import re
import sys

path, wanted = sys.argv[1:]
assignment = re.compile(r"^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$")
with open(path, encoding="utf-8") as handle:
    for raw in handle:
        match = assignment.match(raw.strip())
        if not match or match.group(1) != wanted:
            continue
        value = match.group(2).strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
            if raw.strip().split("=", 1)[1].lstrip().startswith("'"):
                value = value.replace("\\'", "'")
        print(value)
        raise SystemExit(0)
raise SystemExit(1)
PY
}

resolved_data_path() {
  local configured
  configured="$(env_value XG2G_DATA 2>/dev/null || true)"
  printf '%s\n' "${configured:-${DATA_PATH}}"
}

parse_install_root() {
  local -n arguments=$1
  local parsed=()
  local index=0
  while [[ "${index}" -lt "${#arguments[@]}" ]]; do
    case "${arguments[${index}]}" in
      --install-root)
        [[ $((index + 1)) -lt "${#arguments[@]}" ]] || fail "--install-root requires a value"
        INSTALL_ROOT="${arguments[$((index + 1))]%/}"
        [[ -n "${INSTALL_ROOT}" ]] || INSTALL_ROOT="/"
        index=$((index + 2))
        ;;
      *)
        parsed+=("${arguments[${index}]}")
        index=$((index + 1))
        ;;
    esac
  done
  [[ "${INSTALL_ROOT}" == /* ]] || fail "--install-root must be an absolute path"
  arguments=("${parsed[@]}")
}

doctor() {
  local failures=0
  local env_file helper mode endpoint unsafe_listener managed_ca
  local endpoint_curl=()
  env_file="$(host_path "${ENV_PATH}")"
  helper="$(host_path "/srv/xg2g/scripts/compose-xg2g.sh")"

  for tool in python3 tar; do
    if command -v "${tool}" >/dev/null 2>&1; then
      info "${tool}: available"
    else
      warn "${tool}: missing"
      failures=$((failures + 1))
    fi
  done

  if [[ -f "${env_file}" ]]; then
    mode="$(stat -c '%a' "${env_file}" 2>/dev/null || stat -f '%Lp' "${env_file}")"
    if [[ "${mode}" == "600" ]]; then
      info "configuration: protected (0600)"
    else
      warn "configuration mode is ${mode}; expected 600"
      failures=$((failures + 1))
    fi
  else
    warn "configuration missing: ${env_file}"
    failures=$((failures + 1))
  fi

  if [[ -x "${helper}" ]]; then
    if [[ "${INSTALL_ROOT}" != "/" ]]; then
      [[ -d "$(host_path "$(resolved_data_path)")" ]] || {
        warn "test-root data directory is missing"
        failures=$((failures + 1))
      }
      info "storage: test-root layout present"
    elif "${helper}" --storage-check; then
      info "storage: valid"
      "${helper}" --storage-layout
    else
      failures=$((failures + 1))
    fi
  else
    warn "compose helper missing: ${helper}"
    failures=$((failures + 1))
  fi

  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
      info "Docker daemon: available"
    else
      warn "Docker daemon: unavailable"
      failures=$((failures + 1))
    fi
    if systemctl is-active --quiet "${SERVICE_NAME}"; then
      info "systemd service: active"
    else
      warn "systemd service: not active"
      failures=$((failures + 1))
    fi
    if curl -fsS --max-time 10 http://127.0.0.1:8088/readyz >/dev/null; then
      info "local readiness: healthy"
    else
      warn "local readiness: failed"
      failures=$((failures + 1))
    fi
    if command -v ss >/dev/null 2>&1; then
      unsafe_listener="$(
        ss -H -ltn '( sport = :8088 )' |
          awk '{print $4}' |
          grep -Ev '^(127\.0\.0\.1|\[::1\]):8088$' || true
      )"
      if [[ -n "${unsafe_listener}" ]]; then
        warn "port 8088 is exposed beyond loopback: ${unsafe_listener}"
        failures=$((failures + 1))
      else
        info "backend listener: loopback-only"
      fi
    else
      warn "ss is unavailable; listener exposure could not be audited"
    fi

    endpoint="$(env_value XG2G_ALLOWED_ORIGINS 2>/dev/null || true)"
    endpoint="${endpoint%%,*}"
    if [[ -n "${endpoint}" ]]; then
      managed_ca="/etc/xg2g/https-ca.crt"
      if [[ -f "${managed_ca}" ]]; then
        endpoint_curl=(--cacert "${managed_ca}")
      else
        managed_ca="/var/lib/xg2g-caddy/data/caddy/pki/authorities/local/root.crt"
      fi
      if [[ "${#endpoint_curl[@]}" -eq 0 && -f "${managed_ca}" && -f /etc/xg2g/Caddyfile ]] &&
        grep -Eq '^[[:space:]]*tls[[:space:]]+internal([[:space:]]|$)' /etc/xg2g/Caddyfile; then
        endpoint_curl=(--cacert "${managed_ca}")
      fi
      if curl -fsS "${endpoint_curl[@]}" --max-time 15 "${endpoint%/}/readyz" >/dev/null; then
        info "published HTTPS readiness: healthy (${endpoint})"
      else
        warn "published HTTPS readiness failed: ${endpoint}"
        failures=$((failures + 1))
      fi
    fi
  fi

  if [[ "${failures}" -ne 0 ]]; then
    fail "doctor found ${failures} blocking problem(s)"
  fi
  info "doctor: all checks passed"
}

backup() {
  local data_root backup_root stamp temp_dir archive archive_base env_file retention suffix=0
  data_root="$(host_path "$(resolved_data_path)")"
  backup_root="$(host_path "${BACKUP_PATH}")"
  env_file="$(host_path "${ENV_PATH}")"
  retention="$(env_value XG2G_BACKUP_RETENTION_DAYS 2>/dev/null || true)"
  retention="${retention:-14}"
  [[ "${retention}" =~ ^[1-9][0-9]*$ ]] || fail "XG2G_BACKUP_RETENTION_DAYS must be a positive integer"
  [[ -d "${data_root}" ]] || fail "data directory does not exist: ${data_root}"
  [[ -f "${env_file}" ]] || fail "configuration does not exist: ${env_file}"

  install -d -m 0700 "${backup_root}"
  stamp="$(date -u +%Y%m%dT%H%M%SZ)"
  temp_dir="$(mktemp -d "${backup_root}/.${stamp}.XXXXXX")"
  archive_base="${backup_root}/xg2g-backup-${stamp}"
  archive="${archive_base}.tar.gz"
  while [[ -e "${archive}" || -e "${archive}.tmp" ]]; do
    suffix=$((suffix + 1))
    archive="${archive_base}-${suffix}.tar.gz"
  done
  trap 'rm -rf "${temp_dir:-}"' RETURN

  python3 - "${data_root}" "${env_file}" "${temp_dir}" <<'PY'
import hashlib
import json
import shutil
import sqlite3
import sys
from pathlib import Path

data_root, env_file, output = map(Path, sys.argv[1:])
state = output / "state"
state.mkdir(parents=True)

for source in sorted(data_root.glob("*.sqlite")):
    destination = state / source.name
    with sqlite3.connect(f"file:{source}?mode=ro", uri=True) as src:
        with sqlite3.connect(destination) as dst:
            src.backup(dst)

for name in ("channels.json", "series_rules.json", "drift_state.json", "last_sweep.json"):
    source = data_root / name
    if source.is_file():
        shutil.copy2(source, state / name)

shutil.copy2(env_file, output / "xg2g.env")
manifest = {"format": 1, "files": {}}
for path in sorted(output.rglob("*")):
    if path.is_file() and path.name != "manifest.json":
        digest = hashlib.sha256(path.read_bytes()).hexdigest()
        manifest["files"][str(path.relative_to(output))] = {"sha256": digest, "size": path.stat().st_size}
(output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
PY

  COPYFILE_DISABLE=1 tar -czf "${archive}.tmp" -C "${temp_dir}" .
  mv "${archive}.tmp" "${archive}"
  chmod 0600 "${archive}"
  rm -rf "${temp_dir}"
  trap - RETURN

  find "${backup_root}" -maxdepth 1 -type f -name 'xg2g-backup-*.tar.gz' -mtime "+${retention}" -delete
  info "backup created: ${archive}"
  printf '%s\n' "${archive}"
}

extract_restore_archive() {
  local archive="$1"
  local root="$2"
  python3 - "${archive}" "${root}" <<'PY'
import shutil
import sys
import tarfile
from pathlib import Path, PurePosixPath

archive = Path(sys.argv[1])
root = Path(sys.argv[2])
seen = set()

with tarfile.open(archive, "r:gz") as bundle:
    members = []
    for member in bundle.getmembers():
        name = member.name
        while name.startswith("./"):
            name = name[2:]
        if name in {"", "."}:
            if member.isdir():
                continue
            raise SystemExit("backup contains an invalid empty member")

        relative = PurePosixPath(name)
        if relative.is_absolute() or any(part in {"", ".", ".."} for part in relative.parts):
            raise SystemExit(f"unsafe backup member: {member.name}")
        if name in seen:
            raise SystemExit(f"duplicate backup member: {name}")
        seen.add(name)

        allowed = (
            name in {"manifest.json", "xg2g.env", "state"}
            or (
                len(relative.parts) == 2
                and relative.parts[0] == "state"
                and relative.parts[1] not in {"", ".", ".."}
            )
        )
        if not allowed:
            raise SystemExit(f"unexpected backup member: {name}")
        if not (member.isdir() or member.isfile()):
            raise SystemExit(f"backup links/devices are forbidden: {name}")
        if member.isdir() and name != "state":
            raise SystemExit(f"unexpected backup directory: {name}")
        members.append((member, relative))

    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    for member, relative in members:
        destination = root.joinpath(*relative.parts)
        if member.isdir():
            destination.mkdir(mode=0o700, parents=True, exist_ok=True)
            continue
        destination.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        source = bundle.extractfile(member)
        if source is None:
            raise SystemExit(f"unable to read backup member: {member.name}")
        with source, destination.open("xb") as output:
            shutil.copyfileobj(source, output)
        destination.chmod(0o600)
PY
}

validate_restore_tree() {
  local root="$1"
  python3 - "${root}" <<'PY'
import hashlib
import json
import sys
from pathlib import Path, PurePosixPath

root = Path(sys.argv[1])
manifest_path = root / "manifest.json"
if not manifest_path.is_file():
    raise SystemExit("backup manifest is missing")
manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
if manifest.get("format") != 1 or not isinstance(manifest.get("files"), dict):
    raise SystemExit("unsupported backup manifest")
expected_files = set(manifest["files"])
actual_files = {
    str(path.relative_to(root))
    for path in root.rglob("*")
    if path.is_file() and path != manifest_path
}
if expected_files != actual_files:
    missing = sorted(expected_files - actual_files)
    extra = sorted(actual_files - expected_files)
    raise SystemExit(f"backup manifest inventory mismatch; missing={missing}, extra={extra}")
if "xg2g.env" not in expected_files:
    raise SystemExit("backup manifest does not contain xg2g.env")
for relative, expected in manifest["files"].items():
    pure = PurePosixPath(relative)
    if (
        pure.is_absolute()
        or any(part in {"", ".", ".."} for part in pure.parts)
        or (
            relative != "xg2g.env"
            and not (len(pure.parts) == 2 and pure.parts[0] == "state")
        )
    ):
        raise SystemExit(f"unsafe backup manifest path: {relative}")
    if not isinstance(expected, dict):
        raise SystemExit(f"invalid backup manifest entry: {relative}")
    path = root.joinpath(*pure.parts)
    if not path.is_file():
        raise SystemExit(f"backup member missing: {relative}")
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != expected.get("sha256"):
        raise SystemExit(f"backup checksum mismatch: {relative}")
    if path.stat().st_size != expected.get("size"):
        raise SystemExit(f"backup size mismatch: {relative}")
PY
}

restore() {
  local archive="$1"
  local confirmed="$2"
  local data_root env_file temp_dir payload_dir was_active=0
  [[ "${confirmed}" == "yes" ]] || fail "restore requires --yes"
  [[ -f "${archive}" ]] || fail "backup archive does not exist: ${archive}"
  require_root

  temp_dir="$(mktemp -d)"
  trap 'rm -rf "${temp_dir:-}"; if [[ "${was_active:-0}" -eq 1 && "${INSTALL_ROOT:-/}" == "/" ]]; then systemctl start "${SERVICE_NAME:-xg2g.service}" || true; fi' RETURN
  payload_dir="${temp_dir}/payload"
  extract_restore_archive "${archive}" "${payload_dir}"
  validate_restore_tree "${payload_dir}"

  if [[ "${INSTALL_ROOT}" == "/" ]] && systemctl is-active --quiet "${SERVICE_NAME}"; then
    was_active=1
    systemctl stop "${SERVICE_NAME}"
  fi
  backup >/dev/null

  data_root="$(host_path "$(resolved_data_path)")"
  env_file="$(host_path "${ENV_PATH}")"
  install -d -m 0750 "${data_root}"
  find "${data_root}" -maxdepth 1 -type f \
    \( -name '*.sqlite' -o -name 'channels.json' -o -name 'series_rules.json' \
    -o -name 'drift_state.json' -o -name 'last_sweep.json' \) -delete
  find "${payload_dir}/state" -maxdepth 1 -type f -exec install -m 0640 '{}' "${data_root}/" ';'
  install -m 0600 "${payload_dir}/xg2g.env" "${env_file}"
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    chown 10001:10001 "${data_root}"
    find "${data_root}" -maxdepth 1 -type f \
      \( -name '*.sqlite' -o -name 'channels.json' -o -name 'series_rules.json' \
      -o -name 'drift_state.json' -o -name 'last_sweep.json' \) \
      -exec chown 10001:10001 '{}' ';'
    if [[ "${was_active}" -eq 1 ]]; then
      systemctl start "${SERVICE_NAME}"
      was_active=0
    fi
  fi
  rm -rf "${temp_dir}"
  trap - RETURN
  info "restore completed and verified: ${archive}"
}

perform_update() {
  local ref="$1"
  local setup previous_ref state_dir
  local setup_args=()
  setup="$(host_path "/srv/xg2g/infra/systemd/setup-linux.sh")"
  state_dir="$(host_path "/var/lib/xg2g/admin")"
  previous_ref="$(tr -d '[:space:]' < "$(host_path "/srv/xg2g/INSTALL_REF")" 2>/dev/null || true)"
  [[ -x "${setup}" ]] || fail "installed setup helper missing: ${setup}"
  install -d -m 0700 "${state_dir}"
  if [[ -n "${previous_ref}" && "${previous_ref}" != "${ref}" ]]; then
    printf '%s\n' "${previous_ref}" > "${state_dir}/previous-ref"
  fi
  if [[ "${INSTALL_ROOT}" != "/" ]]; then
    setup_args+=(--install-root "${INSTALL_ROOT}")
    if [[ -n "${XG2G_ADMIN_SOURCE_DIR:-}" ]]; then
      setup_args+=(--source-dir "${XG2G_ADMIN_SOURCE_DIR}")
    fi
  fi
  "${setup}" --ref "${ref}" --keep-existing "${setup_args[@]}"
}

update_install() {
  local ref="$1"
  local previous_ref
  require_root
  [[ -n "${ref}" ]] || fail "update requires --ref TAG_OR_SHA"
  backup >/dev/null
  previous_ref="$(tr -d '[:space:]' < "$(host_path "/srv/xg2g/INSTALL_REF")" 2>/dev/null || true)"
  if perform_update "${ref}"; then
    info "update healthy: ${ref}"
    return 0
  fi
  if [[ -n "${previous_ref}" ]]; then
    warn "update failed; reinstalling previous ref ${previous_ref}"
    perform_update "${previous_ref}" || fail "automatic rollback failed; inspect journalctl -u xg2g"
  fi
  fail "update failed and previous version was restored"
}

rollback_install() {
  local confirmed="$1"
  local state_file ref
  [[ "${confirmed}" == "yes" ]] || fail "rollback requires --yes"
  state_file="$(host_path "/var/lib/xg2g/admin/previous-ref")"
  [[ -f "${state_file}" ]] || fail "no previous ref is recorded"
  ref="$(tr -d '[:space:]' < "${state_file}")"
  update_install "${ref}"
}

uninstall_runtime() {
  local purge="$1"
  local confirmed="$2"
  require_root
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    systemctl disable --now xg2g-caddy.service xg2g-backup.timer xg2g-verifier.timer "${SERVICE_NAME}" 2>/dev/null || true
    if [[ -x /srv/xg2g/scripts/compose-xg2g.sh ]]; then
      /srv/xg2g/scripts/compose-xg2g.sh down --remove-orphans || true
    fi
  fi

  for path in \
    /etc/systemd/system/xg2g.service \
    /etc/systemd/system/xg2g-caddy.service \
    /etc/systemd/system/xg2g-backup.service \
    /etc/systemd/system/xg2g-backup.timer \
    /etc/systemd/system/xg2g-verifier.service \
    /etc/systemd/system/xg2g-verifier.timer; do
    rm -f "$(host_path "${path}")"
  done
  rm -f "$(host_path "/usr/local/sbin/xg2g-admin")"
  if [[ -d "$(host_path "/srv/xg2g")" ]]; then
    find "$(host_path "/srv/xg2g")" -depth -mindepth 1 -delete
    rmdir "$(host_path "/srv/xg2g")"
  fi

  if [[ "${purge}" == "yes" ]]; then
    [[ "${confirmed}" == "yes" ]] || fail "--purge-data requires --yes"
    for path in /etc/xg2g /var/lib/xg2g /var/lib/xg2g-caddy /var/backups/xg2g; do
      if [[ -d "$(host_path "${path}")" ]]; then
        find "$(host_path "${path}")" -depth -mindepth 1 -delete
        rmdir "$(host_path "${path}")"
      fi
    done
    info "runtime, configuration, local state, and local backups removed"
  else
    info "runtime removed; configuration, data, backups, recordings, and external DVR storage were preserved"
  fi

  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    systemctl daemon-reload
  fi
}

main() {
  local command="${1:-}"
  shift || true
  local args=("$@")
  parse_install_root args

  case "${command}" in
    doctor)
      [[ "${#args[@]}" -eq 0 ]] || fail "doctor accepts no positional arguments"
      doctor
      ;;
    backup)
      [[ "${#args[@]}" -eq 0 ]] || fail "backup accepts no positional arguments"
      require_root
      backup
      ;;
    restore)
      [[ "${#args[@]}" -eq 2 && "${args[1]}" == "--yes" ]] ||
        fail "usage: xg2g-admin restore ARCHIVE --yes"
      restore "${args[0]}" "yes"
      ;;
    update)
      [[ "${#args[@]}" -eq 2 && "${args[0]}" == "--ref" && -n "${args[1]}" ]] ||
        fail "usage: xg2g-admin update --ref TAG_OR_SHA"
      update_install "${args[1]}"
      ;;
    rollback)
      [[ "${#args[@]}" -eq 1 && "${args[0]}" == "--yes" ]] ||
        fail "usage: xg2g-admin rollback --yes"
      rollback_install "yes"
      ;;
    uninstall)
      case "${#args[@]}:${args[*]:-}" in
        0:) uninstall_runtime "no" "no" ;;
        2:"--purge-data --yes"|2:"--yes --purge-data") uninstall_runtime "yes" "yes" ;;
        *) fail "usage: xg2g-admin uninstall [--purge-data --yes]" ;;
      esac
      ;;
    --help|-h|"")
      usage
      ;;
    *)
      fail "unknown command: ${command}"
      ;;
  esac
}

main "$@"
