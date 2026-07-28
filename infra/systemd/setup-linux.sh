#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_ROOT="/"
SOURCE_REF=""
SOURCE_DIR_OVERRIDE=""
NON_INTERACTIVE=0
KEEP_EXISTING=0
NO_START=0
SOURCE_MODE="git"
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
HTTPS_CA_FILE=""
TRUSTED_PROXIES=""
PROXY_MODE="existing"
CADDY_BIND_IP=""
CONNECTIVITY_PROFILE="lan"
PUBLISHED_ENDPOINTS=""
DVR_WINDOW=""
CONCURRENT_STREAMS="1"
REQUIRED_DVR_GIB=""
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
  --source-dir <dir>    Use an unpacked source tree directly. Only accepted
                        with a non-root --install-root (tests/packaging).
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
  XG2G_SETUP_HTTPS_CA_FILE       Optional CA certificate for an existing proxy
  XG2G_SETUP_TRUSTED_PROXIES     Optional for *_proxy; same-host loopback only
  XG2G_SETUP_PROXY_MODE          existing | caddy_public | caddy_internal
  XG2G_SETUP_CADDY_BIND_IP       Optional exact LAN/VPN listen IP for Caddy
  XG2G_SETUP_DVR_WINDOW          Default: 2h
  XG2G_SETUP_CONCURRENT_STREAMS  Expected simultaneous streams; default: 1
  XG2G_SETUP_STORAGE_MODE        shared | dedicated
  XG2G_SETUP_HLS_ROOT            Required for dedicated; absolute mounted path
  XG2G_SETUP_ALLOW_LOW_SPACE     true only to accept a capacity warning
  XG2G_SETUP_ALLOW_NONPERSISTENT_MOUNT
                                 true only to accept a mount absent from fstab
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
  local tool="$1"
  local distro="unknown"
  command -v "${tool}" >/dev/null 2>&1 && return 0
  if [[ -f /etc/os-release ]]; then
    # shellcheck disable=SC1091
    . /etc/os-release
    distro="${ID_LIKE:-${ID:-unknown}}"
  fi
  case "${distro}" in
    *debian*|*ubuntu*)
      warn "install prerequisites with: sudo apt-get update && sudo apt-get install -y git curl openssl python3 util-linux iproute2 tar"
      ;;
    *fedora*|*rhel*)
      warn "install prerequisites with: sudo dnf install -y git curl openssl python3 util-linux iproute tar"
      ;;
    *arch*)
      warn "install prerequisites with: sudo pacman -S --needed git curl openssl python util-linux iproute2 tar"
      ;;
  esac
  if [[ "${tool}" == "docker" ]]; then
    warn "Docker Engine with the Compose v2 plugin is required: https://docs.docker.com/engine/install/"
  fi
  fail "required tool not found: ${tool}"
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
      --source-dir)
        [[ "$#" -ge 2 ]] || fail "--source-dir requires a value"
        SOURCE_DIR_OVERRIDE="$2"
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
  if [[ -n "${SOURCE_DIR_OVERRIDE}" ]]; then
    [[ "${SOURCE_DIR_OVERRIDE}" == /* ]] || fail "--source-dir must be an absolute path"
    [[ -d "${SOURCE_DIR_OVERRIDE}" ]] || fail "--source-dir does not exist: ${SOURCE_DIR_OVERRIDE}"
    [[ "${INSTALL_ROOT}" != "/" ]] || fail "--source-dir is restricted to non-root test/packaging installs"
  fi
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
  gib="$(python3 - "${hours}" "${CONCURRENT_STREAMS}" <<'PY'
import sys
hours = float(sys.argv[1])
streams = int(sys.argv[2])
print(f"{20 * hours * 0.419 * 1.2 * streams:.1f}")
PY
)"
  REQUIRED_DVR_GIB="${gib}"
  info "DVR sizing: ${CONCURRENT_STREAMS} active ${DVR_WINDOW} stream(s) at 20 Mbit/s need about ${gib} GiB including 20% headroom"
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
        HTTPS_CA_FILE="${XG2G_SETUP_HTTPS_CA_FILE:-}"
        TRUSTED_PROXIES="${XG2G_SETUP_TRUSTED_PROXIES:-}"
        PROXY_MODE="${XG2G_SETUP_PROXY_MODE:-existing}"
        CADDY_BIND_IP="${XG2G_SETUP_CADDY_BIND_IP:-}"
      else
        prompt_text HTTPS_ORIGIN "HTTPS address opened in the browser (for example https://tv.example.net): "
        printf '\nHTTPS edge:\n'
        printf '  1) Use an existing same-host Caddy/nginx/Traefik proxy\n'
        printf '  2) Let xg2g manage Caddy with a public ACME certificate\n'
        printf '  3) Let xg2g manage Caddy with an internal CA (LAN/VPN)\n'
        prompt_text PROXY_MODE "Choose [1]: " "1"
        case "${PROXY_MODE}" in
          1) PROXY_MODE="existing" ;;
          2) PROXY_MODE="caddy_public" ;;
          3) PROXY_MODE="caddy_internal" ;;
        esac
        if [[ "${PROXY_MODE}" == "existing" ]]; then
          prompt_text HTTPS_CA_FILE "Private CA certificate path, if needed [none]: "
        elif [[ "${PROXY_MODE}" == "caddy_internal" ]]; then
          prompt_text CADDY_BIND_IP "Exact LAN/VPN server IP to listen on [all interfaces]: "
        fi
        prompt_text TRUSTED_PROXIES "Proxy IP/CIDR as seen by xg2g [127.0.0.1/32,::1/128]: " "127.0.0.1/32,::1/128"
      fi
      validate_https_origin "${HTTPS_ORIGIN}" || fail "the browser address must be one HTTPS origin without a path"
      validate_cidr_csv "${TRUSTED_PROXIES}" || fail "trusted proxies must be valid IP addresses or CIDRs"
      [[ "${TRUSTED_PROXIES}" == "127.0.0.1/32,::1/128" ]] ||
        fail "the standard install binds xg2g to loopback; the HTTPS proxy must run on this host"
      HTTPS_ORIGIN="${HTTPS_ORIGIN%/}"
      if [[ -n "${HTTPS_CA_FILE}" ]]; then
        validate_absolute_path "HTTPS CA file" "${HTTPS_CA_FILE}"
        [[ -f "${HTTPS_CA_FILE}" || "${INSTALL_ROOT}" != "/" ]] ||
          fail "HTTPS CA file does not exist: ${HTTPS_CA_FILE}"
      fi
      if [[ -n "${CADDY_BIND_IP}" ]]; then
        python3 - "${CADDY_BIND_IP}" <<'PY' || fail "managed Caddy bind value must be one IP address"
import ipaddress
import sys
ipaddress.ip_address(sys.argv[1])
PY
      fi
      case "${PROXY_MODE}" in
        existing) ;;
        caddy_internal)
          python3 - "${HTTPS_ORIGIN}" <<'PY' || fail "managed internal Caddy currently requires the standard HTTPS port 443"
import sys
from urllib.parse import urlsplit

raise SystemExit(0 if urlsplit(sys.argv[1]).port in (None, 443) else 1)
PY
          ;;
        caddy_public)
          python3 - "${HTTPS_ORIGIN}" <<'PY' || fail "managed public Caddy requires a DNS hostname, not an IP address"
import ipaddress
import sys
from urllib.parse import urlsplit

parsed = urlsplit(sys.argv[1])
host = parsed.hostname
if parsed.port not in (None, 443):
    raise SystemExit(1)
try:
    ipaddress.ip_address(host)
except ValueError:
    raise SystemExit(0)
raise SystemExit(1)
PY
          ;;
        *) fail "proxy mode must be existing, caddy_public, or caddy_internal" ;;
      esac
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

prepare_managed_proxy() {
  local caddy_file caddy_data caddy_config site_address tls_line=""
  [[ "${PROXY_MODE}" == caddy_* ]] || return 0

  caddy_file="$(host_path "/etc/xg2g/Caddyfile")"
  caddy_data="$(host_path "/var/lib/xg2g-caddy/data")"
  caddy_config="$(host_path "/var/lib/xg2g-caddy/config")"
  site_address="$(python3 - "${HTTPS_ORIGIN}" <<'PY'
import ipaddress
import sys
from urllib.parse import urlsplit

host = urlsplit(sys.argv[1]).hostname
try:
    if ipaddress.ip_address(host).version == 6:
        host = f"[{host}]"
except ValueError:
    pass
print(host)
PY
)"
  [[ -n "${site_address}" ]] || fail "unable to resolve HTTPS hostname"
  if [[ "${PROXY_MODE}" == "caddy_internal" ]]; then
    tls_line=$'\ttls internal'
  fi

  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    require_tool ss
    if ss -H -ltn '( sport = :80 or sport = :443 )' | grep -q .; then
      fail "ports 80/443 are already in use; choose the existing-proxy option or stop the conflicting service"
    fi
  fi

  install -d -m 0750 "$(dirname "${caddy_file}")" "${caddy_data}" "${caddy_config}"
  {
    printf '{\n\tadmin off\n}\n\n'
    printf '%s {\n' "${site_address}"
    [[ -z "${tls_line}" ]] || printf '%s\n' "${tls_line}"
    [[ -z "${CADDY_BIND_IP}" ]] || printf '\tbind %s\n' "${CADDY_BIND_IP}"
    printf '\treverse_proxy 127.0.0.1:8088\n'
    printf '\trequest_body {\n\t\tmax_size 8MB\n\t}\n'
    printf '}\n'
  } > "${caddy_file}"
  chmod 0640 "${caddy_file}"
}

prepare_https_ca() {
  local target
  [[ -n "${HTTPS_CA_FILE}" ]] || return 0
  target="$(host_path "/etc/xg2g/https-ca.crt")"
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    if [[ "${HTTPS_CA_FILE}" != "${target}" ]]; then
      install -m 0644 "${HTTPS_CA_FILE}" "${target}"
    else
      chmod 0644 "${target}"
    fi
    HTTPS_CA_FILE="/etc/xg2g/https-ca.crt"
  else
    warn "custom HTTPS CA copy is skipped for test install root"
  fi
}

existing_ancestor() {
  local path="$1"
  while [[ ! -e "${path}" && "${path}" != "/" ]]; do
    path="$(dirname "${path}")"
  done
  printf '%s\n' "${path}"
}

check_storage_capacity() {
  local probe available_kib available_gib enough
  [[ "${INSTALL_ROOT}" == "/" ]] || return 0
  probe="$(existing_ancestor "${HLS_ROOT}")"
  available_kib="$(df -Pk "${probe}" | awk 'NR == 2 {print $4}')"
  [[ "${available_kib}" =~ ^[0-9]+$ ]] || fail "could not determine free space for ${probe}"
  available_gib="$(python3 - "${available_kib}" <<'PY'
import sys
print(f"{int(sys.argv[1]) / 1024 / 1024:.1f}")
PY
)"
  enough="$(python3 - "${available_gib}" "${REQUIRED_DVR_GIB}" <<'PY'
import sys
print("yes" if float(sys.argv[1]) >= float(sys.argv[2]) else "no")
PY
)"
  info "DVR capacity: ${available_gib} GiB available, ${REQUIRED_DVR_GIB} GiB estimated"
  [[ "${enough}" == "yes" ]] && return 0
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    [[ "${XG2G_SETUP_ALLOW_LOW_SPACE:-false}" == "true" ]] ||
      fail "insufficient DVR capacity; choose another path or explicitly set XG2G_SETUP_ALLOW_LOW_SPACE=true"
  elif ! prompt_yes_no "Estimated DVR demand exceeds free space. Continue anyway? [y/N]: " "n"; then
    fail "choose a larger DVR path or shorter rewind window"
  fi
  warn "continuing with less free space than the configured DVR estimate"
}

check_mount_persistence() {
  local mount_target
  [[ "${STORAGE_MODE}" == "dedicated" && "${INSTALL_ROOT}" == "/" ]] || return 0
  require_tool findmnt
  mount_target="$(findmnt -T "$(existing_ancestor "${HLS_ROOT}")" -n -o TARGET | head -n 1)"
  [[ -n "${mount_target}" && "${mount_target}" != "/" ]] ||
    fail "dedicated DVR storage must resolve to a non-root mount"
  if findmnt --fstab --target "${mount_target}" >/dev/null 2>&1; then
    info "dedicated mount is declared persistently: ${mount_target}"
    return 0
  fi
  if systemctl list-unit-files --type=mount --no-legend 2>/dev/null |
    awk '{print $1}' | grep -Fxq "$(systemd-escape --path --suffix=mount "${mount_target}")"; then
    info "dedicated mount has a systemd mount unit: ${mount_target}"
    return 0
  fi
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    [[ "${XG2G_SETUP_ALLOW_NONPERSISTENT_MOUNT:-false}" == "true" ]] ||
      fail "${mount_target} is mounted now but has no fstab/systemd persistence; configure it or explicitly allow the risk"
  elif ! prompt_yes_no "${mount_target} is not declared in fstab/systemd and may disappear after reboot. Continue? [y/N]: " "n"; then
    fail "make the dedicated mount persistent before installing"
  fi
  warn "dedicated DVR mount persistence was not proven"
}

choose_storage() {
  if [[ "${NON_INTERACTIVE}" -eq 1 ]]; then
    DVR_WINDOW="${XG2G_SETUP_DVR_WINDOW:-2h}"
    CONCURRENT_STREAMS="${XG2G_SETUP_CONCURRENT_STREAMS:-1}"
    STORAGE_MODE="${XG2G_SETUP_STORAGE_MODE:-shared}"
  else
    printf '\nDVR scratch stores temporary live segments, not permanent recordings.\n'
    prompt_text DVR_WINDOW "How far should live TV rewind? [2h]: " "2h"
    prompt_text CONCURRENT_STREAMS "Maximum simultaneous live streams [1]: " "1"
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
  [[ "${CONCURRENT_STREAMS}" =~ ^[1-9][0-9]*$ ]] ||
    fail "simultaneous streams must be a positive whole number"
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

  check_storage_capacity
  check_mount_persistence
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

  if [[ -n "${SOURCE_DIR_OVERRIDE}" ]]; then
    REPO_ROOT="${SOURCE_DIR_OVERRIDE%/}"
    SOURCE_MODE="bundle"
    [[ -n "${SOURCE_REF}" ]] || SOURCE_REF="working-tree"
    return 0
  fi

  if git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    if [[ -z "${SOURCE_REF}" ]]; then
      SOURCE_REF="$(git -C "${REPO_ROOT}" rev-parse HEAD)"
    fi
    return 0
  fi

  [[ -f "${REPO_ROOT}/backend/VERSION" ]] || fail "download has no Git metadata or backend/VERSION"
  version="$(tr -d '[:space:]' < "${REPO_ROOT}/backend/VERSION")"
  if [[ -x "${REPO_ROOT}/xg2g" && -x "${REPO_ROOT}/infra/systemd/sync.sh" ]]; then
    if [[ -n "${SOURCE_REF}" && "${SOURCE_REF}" != "${version}" ]]; then
      fail "release archive version is ${version}; refusing mismatched --ref ${SOURCE_REF}"
    fi
    SOURCE_REF="${version}"
    SOURCE_MODE="bundle"
    info "official release bundle detected: ${SOURCE_REF}"
    return 0
  fi

  if [[ -z "${SOURCE_REF}" ]]; then
    fail "GitHub source ZIPs are not immutable install artifacts. Download the Linux archive from https://github.com/ManuGH/xg2g/releases or use 'git clone https://github.com/ManuGH/xg2g.git'."
  fi

  require_tool git
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
  local source_args=()
  case "${GPU_MODE}" in
    vaapi) gpu_overlay="enable" ;;
    nvidia) nvidia_overlay="enable" ;;
    preserve)
      gpu_overlay="auto"
      nvidia_overlay="auto"
      ;;
  esac

  if [[ "${SOURCE_MODE}" == "bundle" ]]; then
    source_args=(--source-dir "${REPO_ROOT}")
  fi

  "${REPO_ROOT}/infra/systemd/sync.sh" \
    --apply \
    --ref "${SOURCE_REF}" \
    --repo-root "${REPO_ROOT}" \
    "${source_args[@]}" \
    --install-root "${INSTALL_ROOT}" \
    --gpu-overlay "${gpu_overlay}" \
    --nvidia-overlay "${nvidia_overlay}" \
    --verifier-bundle enable
}

start_service() {
  local helper attempt ca_cert headers_file connectivity_file
  local curl_tls=()
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
  systemctl enable --now xg2g-backup.timer xg2g-verifier.timer
  if [[ -f /etc/xg2g/Caddyfile ]]; then
    docker pull caddy:2.11.4-alpine
    systemctl enable --now xg2g-caddy.service
  fi
  "${helper}" --storage-layout
  curl -fsS --max-time 10 http://127.0.0.1:8088/readyz >/dev/null ||
    fail "xg2g started but readiness failed; run: journalctl -u xg2g -n 100 --no-pager"
  if [[ -n "${HTTPS_ORIGIN}" ]]; then
    ca_cert="/var/lib/xg2g-caddy/data/caddy/pki/authorities/local/root.crt"
    attempt=0
    while [[ "${attempt}" -lt 30 ]]; do
      if [[ "${PROXY_MODE}" == "caddy_internal" && -f "${ca_cert}" ]]; then
        curl -fsS --cacert "${ca_cert}" --max-time 10 "${HTTPS_ORIGIN}/readyz" >/dev/null && break
      elif [[ -n "${HTTPS_CA_FILE}" ]]; then
        curl -fsS --cacert "${HTTPS_CA_FILE}" --max-time 10 "${HTTPS_ORIGIN}/readyz" >/dev/null && break
      elif [[ "${PROXY_MODE}" != "caddy_internal" ]]; then
        curl -fsS --max-time 10 "${HTTPS_ORIGIN}/readyz" >/dev/null && break
      fi
      sleep 2
      attempt=$((attempt + 1))
    done
    [[ "${attempt}" -lt 30 ]] ||
      fail "local service is healthy but HTTPS validation failed for ${HTTPS_ORIGIN}; inspect the proxy and DNS/certificate configuration"

    if [[ "${PROXY_MODE}" == "caddy_internal" ]]; then
      curl_tls=(--cacert "${ca_cert}")
    elif [[ -n "${HTTPS_CA_FILE}" ]]; then
      curl_tls=(--cacert "${HTTPS_CA_FILE}")
    fi
    headers_file="$(mktemp)"
    connectivity_file="$(mktemp)"
    curl -fsS "${curl_tls[@]}" --max-time 15 -D "${headers_file}" \
      -o /dev/null "${HTTPS_ORIGIN}/ui/" ||
      fail "managed HTTPS WebUI smoke failed"
    grep -Eiq '^Strict-Transport-Security:' "${headers_file}" ||
      fail "managed HTTPS reached xg2g without HSTS; proxy trust is incorrect"
    curl -fsS "${curl_tls[@]}" --max-time 15 \
      -H "Authorization: Bearer ${NEW_API_TOKEN}" \
      -o "${connectivity_file}" "${HTTPS_ORIGIN}/api/v3/system/connectivity" ||
      fail "managed HTTPS connectivity smoke failed"
    python3 - "${connectivity_file}" "${HTTPS_ORIGIN}" <<'PY' ||
      fail "managed HTTPS connectivity contract is not safe"
import json
import sys

path, origin = sys.argv[1:]
with open(path, encoding="utf-8") as handle:
    payload = json.load(handle)
valid = (
    payload.get("request", {}).get("effectiveHttps") is True
    and payload.get("webBlocked") is False
    and origin in payload.get("allowedOrigins", [])
)
raise SystemExit(0 if valid else 1)
PY
    rm -f "${headers_file}" "${connectivity_file}"
  fi
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
  printf '  Diagnose: sudo xg2g-admin doctor\n'
  printf '  Backup now: sudo xg2g-admin backup\n'
  printf '  Storage report: /srv/xg2g/scripts/compose-xg2g.sh --storage-layout\n'
  if [[ -n "${NEW_API_TOKEN}" ]]; then
    printf '\nSave this admin token now; it is shown only by this setup run:\n'
    printf '  %s\n' "${NEW_API_TOKEN}"
  fi
  if [[ "${ACCESS_MODE}" == "local" ]]; then
    printf '\nRemote browsers require HTTPS. Re-run with an existing HTTPS proxy configured,\n'
    printf 'or follow infra/systemd/REVERSE_PROXY.md.\n'
  fi
  if [[ "${PROXY_MODE}" == "caddy_internal" ]]; then
    printf '\nInstall this CA certificate on every client before opening the WebUI:\n'
    printf '  /var/lib/xg2g-caddy/data/caddy/pki/authorities/local/root.crt\n'
  fi
}

main() {
  local env_file
  parse_args "$@"

  [[ "$(uname -s)" == "Linux" || "${INSTALL_ROOT}" != "/" ]] || fail "this setup wizard supports Linux hosts only"
  if [[ "${INSTALL_ROOT}" == "/" && "${EUID}" -ne 0 ]]; then
    fail "run this first-time host setup with sudo"
  fi

  require_tool install
  require_tool python3
  require_tool openssl
  require_tool curl
  if git -C "${REPO_ROOT}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
    require_tool git
  fi
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
    prepare_https_ca
    prepare_managed_proxy
  fi

  info "installing pinned source ${SOURCE_REF}"
  run_sync
  start_service
  print_handoff
}

main "$@"
