#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_ROOT="/"
SOURCE_REF=""
NON_INTERACTIVE=0
KEEP_EXISTING=0
NO_START=0
TEMP_DIRS=()

ENV_TARGET="/etc/xg2g/xg2g.env"
DATA_ROOT="/var/lib/xg2g"
DEFAULT_HLS_ROOT="/var/lib/xg2g/hls"
RECORDINGS_ROOT="/media/nfs-recordings"

NEW_API_TOKEN=""
E2_HOST=""
E2_USER=""
E2_PASS=""
ACCESS_MODE=""
HTTPS_ORIGIN=""
TRUSTED_PROXIES=""
CONNECTIVITY_PROFILE="lan"
PUBLISHED_ENDPOINTS=""
DVR_WINDOW=""
STORAGE_MODE=""
HLS_ROOT=""
HLS_REQUIRE_MOUNT="false"
GPU_MODE=""

usage() {
  cat <<'EOF'
Usage:
  sudo ./infra/systemd/setup-linux.sh [options]

Guided first-time installation for a Linux host with systemd and Docker. The
wizard creates the secure environment file and storage directories, then
delegates deployment to the canonical pinned infra/systemd/sync.sh path.

Options:
  --ref <tag|sha>       Install an explicit Git tag or commit. The current
                        checkout commit is used when omitted.
  --install-root <dir>  Prefix installed paths (test/packaging use only).
  --non-interactive     Read answers from XG2G_SETUP_* environment variables.
  --keep-existing       Keep an existing /etc/xg2g/xg2g.env unchanged.
  --no-start            Install and verify, but do not enable or start xg2g.
  --help                Show this help.

Non-interactive variables:
  XG2G_SETUP_E2_HOST             Required receiver URL (http:// or https://)
  XG2G_SETUP_E2_USER             Optional OpenWebIF username
  XG2G_SETUP_E2_PASS             Optional OpenWebIF password
  XG2G_SETUP_ACCESS_MODE         local | private_proxy | public_proxy
  XG2G_SETUP_HTTPS_ORIGIN        Required for *_proxy (https://host[:port])
  XG2G_SETUP_TRUSTED_PROXIES     Optional for *_proxy; same-host loopback only
  XG2G_SETUP_DVR_WINDOW          Default: 2h
  XG2G_SETUP_STORAGE_MODE        shared | dedicated
  XG2G_SETUP_HLS_ROOT            Required for dedicated; absolute mounted path
  XG2G_SETUP_GPU_MODE            auto | none | vaapi | nvidia

This script never partitions, formats, mounts, or deletes a storage device.
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

cleanup() {
  local dir
  for dir in "${TEMP_DIRS[@]:-}"; do
    [[ -n "${dir}" && -d "${dir}" ]] && rm -rf "${dir}"
  done
  return 0
}
trap cleanup EXIT

host_path() {
  local path="$1"
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    printf '%s\n' "${path}"
  else
    printf '%s%s\n' "${INSTALL_ROOT%/}" "${path}"
  fi
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "required tool not found: $1"
}

parse_args() {
  while [[ "$#" -gt 0 ]]; do
    case "$1" in
      --ref)
        [[ "$#" -ge 2 ]] || fail "--ref requires a value"
        SOURCE_REF="$2"
        shift 2
        ;;
      --install-root)
        [[ "$#" -ge 2 ]] || fail "--install-root requires a value"
        INSTALL_ROOT="${2%/}"
        [[ -n "${INSTALL_ROOT}" ]] || INSTALL_ROOT="/"
        shift 2
        ;;
      --non-interactive)
        NON_INTERACTIVE=1
        shift
        ;;
      --keep-existing)
        KEEP_EXISTING=1
        shift
        ;;
      --no-start)
        NO_START=1
        shift
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

  [[ "${INSTALL_ROOT}" == /* ]] || fail "--install-root must be an absolute path"
}

prompt_text() {
  local destination="$1"
  local prompt="$2"
  local default_value="${3:-}"
  local secret="${4:-false}"
  local answer=""

  if [[ "${secret}" == "true" ]]; then
    read -r -s -p "${prompt}" answer
    printf '\n'
  else
    read -r -p "${prompt}" answer
  fi
  if [[ -z "${answer}" ]]; then
    answer="${default_value}"
  fi
  printf -v "${destination}" '%s' "${answer}"
}

prompt_yes_no() {
  local prompt="$1"
  local default_answer="$2"
  local answer=""

  read -r -p "${prompt}" answer
  answer="${answer:-${default_answer}}"
  case "${answer,,}" in
    y|yes|j|ja) return 0 ;;
    n|no|nein) return 1 ;;
    *) warn "please answer yes or no"; prompt_yes_no "${prompt}" "${default_answer}" ;;
  esac
}

validate_single_line() {
  local label="$1"
  local value="$2"
  [[ "${value}" != *$'\n'* && "${value}" != *$'\r'* ]] || fail "${label} must be a single line"
}

validate_receiver_url() {
  local value="$1"
  python3 - "${value}" <<'PY'
import sys
from urllib.parse import urlsplit

value = sys.argv[1]
parsed = urlsplit(value)
valid = (
    parsed.scheme in {"http", "https"}
    and bool(parsed.hostname)
    and parsed.username is None
    and parsed.password is None
    and not parsed.query
    and not parsed.fragment
)
if not valid:
    raise SystemExit(1)
PY
}

validate_https_origin() {
  local value="$1"
  python3 - "${value}" <<'PY'
import sys
from urllib.parse import urlsplit

value = sys.argv[1]
parsed = urlsplit(value)
valid = (
    parsed.scheme == "https"
    and bool(parsed.hostname)
    and parsed.username is None
    and parsed.password is None
    and parsed.path in {"", "/"}
    and not parsed.query
    and not parsed.fragment
)
if not valid:
    raise SystemExit(1)
PY
}

validate_cidr_csv() {
  local value="$1"
  python3 - "${value}" <<'PY'
import ipaddress
import sys

values = [part.strip() for part in sys.argv[1].split(",") if part.strip()]
if not values:
    raise SystemExit(1)
for value in values:
    try:
        ipaddress.ip_network(value, strict=False)
    except ValueError:
        try:
            ipaddress.ip_address(value)
        except ValueError:
            raise SystemExit(1)
PY
}

validate_duration() {
  local value="$1"
  [[ "${value}" =~ ^[1-9][0-9]*(s|m|h)$ ]] || fail "DVR window must look like 45m, 2h, or 7200s"
}

validate_absolute_path() {
  local label="$1"
  local value="$2"
  [[ "${value}" == /* ]] || fail "${label} must be an absolute Linux path"
  [[ "${value}" != "/" ]] || fail "${label} must not be the filesystem root"
  case "/${value#/}/" in
    *"/../"*|*"/./"*|*"//"*) fail "${label} must be a normalized path" ;;
  esac
}

print_storage_candidates() {
  local target fs_type free
  command -v findmnt >/dev/null 2>&1 || {
    warn "findmnt is unavailable; mounted-storage inventory cannot be displayed"
    return 0
  }

  printf '\nMounted paths that may be suitable for DVR scratch:\n'
  printf '%-34s %-14s %-10s\n' "MOUNT" "FILESYSTEM" "FREE"
  while read -r target fs_type; do
    case "${fs_type}" in
      proc|sysfs|devtmpfs|devpts|cgroup*|securityfs|pstore|tracefs|debugfs|configfs|fusectl|mqueue|autofs|overlay|squashfs)
        continue
        ;;
    esac
    case "${target}" in
      /boot*|/snap*|/run*|/sys*|/proc*|/dev*) continue ;;
    esac
    free="$(df -hP "${target}" 2>/dev/null | awk 'NR == 2 {print $4}')"
    printf '%-34s %-14s %-10s\n' "${target}" "${fs_type}" "${free:--}"
  done < <(findmnt -rn -o TARGET,FSTYPE)
  printf '\n'
}

duration_hours() {
  python3 - "$1" <<'PY'
import re
import sys

match = re.fullmatch(r"([1-9][0-9]*)(s|m|h)", sys.argv[1])
if not match:
    raise SystemExit(1)
value = int(match.group(1))
factor = {"s": 1 / 3600, "m": 1 / 60, "h": 1}[match.group(2)]
print(f"{value * factor:.3f}")
PY
}

report_capacity_estimate() {
  local hours gib
  hours="$(duration_hours "${DVR_WINDOW}")"
  gib="$(python3 - "${hours}" <<'PY'
import sys
hours = float(sys.argv[1])
print(f"{20 * hours * 0.419 * 1.2:.1f}")
PY
)"
  info "DVR sizing: at 20 Mbit/s, one active ${DVR_WINDOW} session needs about ${gib} GiB including 20% headroom"
}

choose_access_mode() {
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    ACCESS_MODE="${XG2G_SETUP_ACCESS_MODE:-local}"
  else
    printf '\nHow will browsers reach xg2g?\n'
    printf '  1) Only on this Linux host (http://localhost)\n'
    printf '  2) Privately over LAN/VPN through an existing HTTPS reverse proxy\n'
    printf '  3) Publicly through an existing HTTPS reverse proxy\n'
    prompt_text ACCESS_MODE "Choose [1]: " "1"
    case "${ACCESS_MODE}" in
      1) ACCESS_MODE="local" ;;
      2) ACCESS_MODE="private_proxy" ;;
      3) ACCESS_MODE="public_proxy" ;;
    esac
  fi

  case "${ACCESS_MODE}" in
    local)
      CONNECTIVITY_PROFILE="lan"
      ;;
    private_proxy|public_proxy)
      if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
        HTTPS_ORIGIN="${XG2G_SETUP_HTTPS_ORIGIN:-}"
        TRUSTED_PROXIES="${XG2G_SETUP_TRUSTED_PROXIES:-}"
      else
        prompt_text HTTPS_ORIGIN "HTTPS address opened in the browser (for example https://tv.example.net): "
        prompt_text TRUSTED_PROXIES "Proxy IP/CIDR as seen by xg2g [127.0.0.1/32,::1/128]: " "127.0.0.1/32,::1/128"
      fi
      validate_https_origin "${HTTPS_ORIGIN}" || fail "the browser address must be one HTTPS origin without a path"
      validate_cidr_csv "${TRUSTED_PROXIES}" || fail "trusted proxies must be valid IP addresses or CIDRs"
      [[ "${TRUSTED_PROXIES}" == "127.0.0.1/32,::1/128" ]] ||
        fail "the standard install binds xg2g to loopback; the HTTPS proxy must run on this host"
      HTTPS_ORIGIN="${HTTPS_ORIGIN%/}"
      if [[ "${ACCESS_MODE}" == "public_proxy" ]]; then
        CONNECTIVITY_PROFILE="reverse_proxy"
        PUBLISHED_ENDPOINTS="[{\"url\":\"${HTTPS_ORIGIN}\",\"kind\":\"public_https\",\"priority\":100,\"tlsMode\":\"required\",\"allowPairing\":true,\"allowStreaming\":true,\"allowWeb\":true,\"allowNative\":true,\"source\":\"env\"}]"
      else
        CONNECTIVITY_PROFILE="lan"
        PUBLISHED_ENDPOINTS="[{\"url\":\"${HTTPS_ORIGIN}\",\"kind\":\"local_https\",\"priority\":100,\"tlsMode\":\"required\",\"allowPairing\":true,\"allowStreaming\":true,\"allowWeb\":true,\"allowNative\":true,\"source\":\"env\"}]"
      fi
      ;;
    *)
      fail "access mode must be local, private_proxy, or public_proxy"
      ;;
  esac
}

choose_storage() {
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    DVR_WINDOW="${XG2G_SETUP_DVR_WINDOW:-2h}"
    STORAGE_MODE="${XG2G_SETUP_STORAGE_MODE:-shared}"
  else
    printf '\nDVR scratch stores temporary live segments, not permanent recordings.\n'
    prompt_text DVR_WINDOW "How far should live TV rewind? [2h]: " "2h"
    print_storage_candidates
    printf '  1) Recommended simple setup: use the system/data filesystem\n'
    printf '  2) Use a separate, already mounted HDD/SSD/NVMe path\n'
    prompt_text STORAGE_MODE "Choose [1]: " "1"
    case "${STORAGE_MODE}" in
      1) STORAGE_MODE="shared" ;;
      2) STORAGE_MODE="dedicated" ;;
    esac
  fi

  validate_duration "${DVR_WINDOW}"
  report_capacity_estimate

  case "${STORAGE_MODE}" in
    shared)
      HLS_ROOT="${DEFAULT_HLS_ROOT}"
      HLS_REQUIRE_MOUNT="false"
      ;;
    dedicated)
      if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
        HLS_ROOT="${XG2G_SETUP_HLS_ROOT:-}"
      else
        prompt_text HLS_ROOT "Absolute DVR scratch directory (for example /mnt/hdd/xg2g-hls): "
      fi
      validate_absolute_path "DVR scratch path" "${HLS_ROOT}"
      HLS_REQUIRE_MOUNT="true"
      ;;
    *)
      fail "storage mode must be shared or dedicated"
      ;;
  esac
}

choose_gpu_mode() {
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    GPU_MODE="${XG2G_SETUP_GPU_MODE:-auto}"
  else
    printf '\nHardware video acceleration:\n'
    printf '  1) Auto-detect (recommended)\n'
    printf '  2) CPU only\n'
    printf '  3) Intel/AMD VAAPI (/dev/dri)\n'
    printf '  4) NVIDIA (NVIDIA Container Toolkit required)\n'
    prompt_text GPU_MODE "Choose [1]: " "1"
    case "${GPU_MODE}" in
      1) GPU_MODE="auto" ;;
      2) GPU_MODE="none" ;;
      3) GPU_MODE="vaapi" ;;
      4) GPU_MODE="nvidia" ;;
    esac
  fi

  case "${GPU_MODE}" in
    auto)
      if compgen -G "/dev/dri/renderD*" >/dev/null; then
        GPU_MODE="vaapi"
      elif command -v nvidia-smi >/dev/null 2>&1; then
        GPU_MODE="nvidia"
      else
        GPU_MODE="none"
      fi
      ;;
    none|vaapi|nvidia) ;;
    *) fail "GPU mode must be auto, none, vaapi, or nvidia" ;;
  esac
  info "selected runtime: ${GPU_MODE}"
}

collect_answers() {
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    E2_HOST="${XG2G_SETUP_E2_HOST:-}"
    E2_USER="${XG2G_SETUP_E2_USER:-}"
    E2_PASS="${XG2G_SETUP_E2_PASS:-}"
  else
    printf '\nxg2g Linux setup\n'
    printf 'No disk will be partitioned, formatted, mounted, or deleted.\n\n'
    prompt_text E2_HOST "Enigma2/OpenWebIF URL (for example http://192.168.1.50): "
    if prompt_yes_no "Does OpenWebIF require a username/password? [y/N]: " "n"; then
      prompt_text E2_USER "OpenWebIF username: "
      prompt_text E2_PASS "OpenWebIF password: " "" "true"
    fi
  fi

  validate_single_line "receiver URL" "${E2_HOST}"
  validate_receiver_url "${E2_HOST}" || fail "receiver URL must be http(s)://host without embedded credentials, query, or fragment"
  E2_HOST="${E2_HOST%/}"
  if [[ -n "${E2_USER}" || -n "${E2_PASS}" ]]; then
    [[ -n "${E2_USER}" && -n "${E2_PASS}" ]] || fail "OpenWebIF username and password must be supplied together"
    validate_single_line "OpenWebIF username" "${E2_USER}"
    validate_single_line "OpenWebIF password" "${E2_PASS}"
  fi

  choose_access_mode
  choose_storage
  choose_gpu_mode
}

dotenv_line() {
  local key="$1"
  local value="$2"
  local escaped
  validate_single_line "${key}" "${value}"
  # Compose dotenv single quotes keep $, #, spaces, and backslashes literal.
  # Only an embedded apostrophe needs the documented \' escape.
  escaped=${value//\'/\\\'}
  printf "%s='%s'\n" "${key}" "${escaped}"
}

write_environment() {
  local env_file tmp_file
  env_file="$(host_path "${ENV_TARGET}")"
  tmp_file="$(mktemp)"

  NEW_API_TOKEN="${XG2G_SETUP_API_TOKEN:-$(openssl rand -hex 32)}"
  local decision_secret="${XG2G_SETUP_DECISION_SECRET:-$(openssl rand -hex 32)}"
  [[ "${#NEW_API_TOKEN}" -ge 32 ]] || fail "generated/provided API token must be at least 32 characters"
  [[ "${#decision_secret}" -ge 32 ]] || fail "generated/provided decision secret must be at least 32 characters"

  {
    printf '# Generated by infra/systemd/setup-linux.sh. Keep mode 0600.\n'
    dotenv_line XG2G_E2_HOST "${E2_HOST}"
    if [[ -n "${E2_USER}" ]]; then
      dotenv_line XG2G_E2_USER "${E2_USER}"
      dotenv_line XG2G_E2_PASS "${E2_PASS}"
    fi
    dotenv_line XG2G_API_TOKEN "${NEW_API_TOKEN}"
    dotenv_line XG2G_API_TOKEN_SCOPES "v3:admin"
    dotenv_line XG2G_DECISION_SECRET "${decision_secret}"
    dotenv_line XG2G_HLS_DVR_WINDOW "${DVR_WINDOW}"
    dotenv_line XG2G_HLS_ROOT "${HLS_ROOT}"
    dotenv_line XG2G_HLS_REQUIRE_MOUNT "${HLS_REQUIRE_MOUNT}"
    dotenv_line XG2G_CONNECTIVITY_PROFILE "${CONNECTIVITY_PROFILE}"
    if [[ -n "${HTTPS_ORIGIN}" ]]; then
      dotenv_line XG2G_ALLOWED_ORIGINS "${HTTPS_ORIGIN}"
      dotenv_line XG2G_TRUSTED_PROXIES "${TRUSTED_PROXIES}"
      dotenv_line XG2G_PUBLISHED_ENDPOINTS "${PUBLISHED_ENDPOINTS}"
    fi
  } > "${tmp_file}"

  install -d -m 0755 "$(dirname "${env_file}")"
  install -m 0600 "${tmp_file}" "${env_file}"
  rm -f "${tmp_file}"
}

prepare_storage() {
  local data_path recordings_path hls_path data_mount hls_mount hls_probe
  data_path="$(host_path "${DATA_ROOT}")"
  recordings_path="$(host_path "${RECORDINGS_ROOT}")"
  hls_path="$(host_path "${HLS_ROOT}")"

  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    install -d -m 0750 -o 10001 -g 10001 "${data_path}"
  else
    install -d -m 0750 "${data_path}"
  fi
  install -d -m 0755 "${recordings_path}"

  if [[ "${HLS_REQUIRE_MOUNT}" == "true" && "${INSTALL_ROOT}" == "/" ]]; then
    require_tool findmnt
    data_mount="$(findmnt -T "${DATA_ROOT}" -n -o TARGET | head -n 1)"
    hls_probe="${HLS_ROOT}"
    while [[ ! -e "${hls_probe}" && "${hls_probe}" != "/" ]]; do
      hls_probe="$(dirname "${hls_probe}")"
    done
    hls_mount="$(findmnt -T "${hls_probe}" -n -o TARGET | head -n 1)"
    [[ -n "${data_mount}" && -n "${hls_mount}" && "${data_mount}" != "${hls_mount}" ]] ||
      fail "dedicated DVR path shares the data mount (${data_mount:-unknown}); mount the intended disk first"
  fi
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    install -d -m 0750 -o 10001 -g 10001 "${hls_path}"
  else
    install -d -m 0750 "${hls_path}"
  fi
}

resolve_deploy_source() {
  local version clone_root

  if git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if [[ -z "${SOURCE_REF}" ]]; then
      SOURCE_REF="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
    fi
    return 0
  fi

  [[ -f "${REPO_ROOT}/backend/VERSION" ]] || fail "download has no Git metadata or backend/VERSION"
  version="$(tr -d '[:space:]' < "${REPO_ROOT}/backend/VERSION")"
  if [[ -z "${SOURCE_REF}" ]]; then
    case "${version}" in
      v*) SOURCE_REF="${version}" ;;
      *) SOURCE_REF="v${version}" ;;
    esac
  fi
  clone_root="$(mktemp -d)"
  TEMP_DIRS+=("${clone_root}")
  info "source archive detected; downloading pinned deployment source ${SOURCE_REF}"
  git -c init.defaultBranch=main init --quiet "${clone_root}/xg2g"
  git -C "${clone_root}/xg2g" remote add origin https://github.com/ManuGH/xg2g.git
  git -C "${clone_root}/xg2g" fetch --quiet --depth 1 origin "${SOURCE_REF}" ||
    fail "could not download ${SOURCE_REF}; use a Git checkout or check network access"
  git -C "${clone_root}/xg2g" checkout --quiet --detach FETCH_HEAD
  REPO_ROOT="${clone_root}/xg2g"
}

run_sync() {
  local gpu_overlay="disable"
  local nvidia_overlay="disable"
  case "${GPU_MODE}" in
    vaapi) gpu_overlay="enable" ;;
    nvidia) nvidia_overlay="enable" ;;
    preserve)
      gpu_overlay="auto"
      nvidia_overlay="auto"
      ;;
  esac

  "${REPO_ROOT}/infra/systemd/sync.sh" \
    --apply \
    --ref "${SOURCE_REF}" \
    --repo-root "${REPO_ROOT}" \
    --install-root "${INSTALL_ROOT}" \
    --gpu-overlay "${gpu_overlay}" \
    --nvidia-overlay "${nvidia_overlay}" \
    --verifier-bundle disable
}

start_service() {
  local helper
  [[ "${NO_START}" -eq 0 ]] || {
    info "installation prepared but not started (--no-start)"
    return 0
  }
  [[ "${INSTALL_ROOT}" == "/" ]] || {
    info "test install root prepared; service start skipped"
    return 0
  }

  require_tool docker
  require_tool systemctl
  docker info >/dev/null 2>&1 || fail "Docker daemon is not available"
  docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 plugin is not available"
  helper="/srv/xg2g/scripts/compose-xg2g.sh"
  "${helper}" pull xg2g
  systemctl enable --now xg2g.service
  "${helper}" --storage-layout
  curl -fsS --max-time 10 http://127.0.0.1:8088/readyz >/dev/null ||
    fail "xg2g started but readiness failed; run: journalctl -u xg2g -n 100 --no-pager"
}

print_handoff() {
  local browser_url="http://localhost:8088/ui/"
  if [[ "${KEEP_EXISTING}" -eq 1 ]]; then
    browser_url="configured endpoint (unchanged)"
  elif [[ -n "${HTTPS_ORIGIN}" ]]; then
    browser_url="${HTTPS_ORIGIN}/ui/"
  fi

  printf '\nSetup complete.\n'
  printf '  WebUI: %s\n' "${browser_url}"
  printf '  Config: %s\n' "${ENV_TARGET}"
  printf '  Storage report: /srv/xg2g/scripts/compose-xg2g.sh --storage-layout\n'
  if [[ -n "${NEW_API_TOKEN}" ]]; then
    printf '\nSave this admin token now; it is shown only by this setup run:\n'
    printf '  %s\n' "${NEW_API_TOKEN}"
  fi
  if [[ "${ACCESS_MODE}" == "local" ]]; then
    printf '\nRemote browsers require HTTPS. Re-run with an existing HTTPS proxy configured,\n'
    printf 'or follow infra/systemd/REVERSE_PROXY.md.\n'
  fi
}

main() {
  local env_file
  parse_args "$@"

  [[ "$(uname -s)" == "Linux" || "${INSTALL_ROOT}" != "/" ]] || fail "this setup wizard supports Linux hosts only"
  if [[ "${INSTALL_ROOT}" == "/" && "${EUID}" -ne 0 ]]; then
    fail "run this first-time host setup with sudo"
  fi

  require_tool git
  require_tool install
  require_tool python3
  require_tool openssl
  require_tool curl
  if [[ "${NO_START}" -eq 0 && "${INSTALL_ROOT}" == "/" ]]; then
    require_tool docker
    require_tool systemctl
    docker info >/dev/null 2>&1 || fail "Docker daemon is not available"
    docker compose version >/dev/null 2>&1 || fail "Docker Compose v2 plugin is not available"
  fi

  resolve_deploy_source
  env_file="$(host_path "${ENV_TARGET}")"
  if [[ -f "${env_file}" ]]; then
    if [[ "${KEEP_EXISTING}" -ne 1 ]]; then
      fail "${env_file} already exists; use --keep-existing to repair/upgrade without replacing secrets"
    fi
    info "keeping existing configuration unchanged: ${env_file}"
    GPU_MODE="preserve"
  else
    collect_answers
    prepare_storage
    write_environment
  fi

  info "installing pinned source ${SOURCE_REF}"
  run_sync
  start_service
  print_handoff
}

main "$@"
