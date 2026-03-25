#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

MODE=""
SOURCE_REF=""
INSTALL_ROOT="/"
GPU_OVERLAY_MODE="auto"
VERIFIER_BUNDLE_MODE="auto"
FETCH_REMOTE="origin"

SOURCE_ROOT=""
SOURCE_LABEL=""
SOURCE_COMMIT=""
GPU_OVERLAY_ENABLED=0
VERIFIER_BUNDLE_ENABLED=0
DRIFT_DETECTED=0
TEMP_DIRS=()

CORE_SPECS=(
  "deploy/docker-compose.yml|/srv/xg2g/docker-compose.yml|0644"
  "deploy/xg2g.service|/srv/xg2g/docs/ops/xg2g.service|0644"
  "deploy/xg2g.service|/etc/systemd/system/xg2g.service|0644"
  "backend/scripts/compose-xg2g.sh|/srv/xg2g/scripts/compose-xg2g.sh|0755"
  "backend/scripts/verify-compose-contract.sh|/srv/xg2g/scripts/verify-compose-contract.sh|0755"
  "backend/scripts/verify-installed-unit.sh|/srv/xg2g/scripts/verify-installed-unit.sh|0755"
  "backend/scripts/verify-systemd-runtime-contract.sh|/srv/xg2g/scripts/verify-systemd-runtime-contract.sh|0755"
  "backend/scripts/verify-installation-contract.sh|/srv/xg2g/scripts/verify-installation-contract.sh|0755"
)

GPU_SPECS=(
  "deploy/docker-compose.gpu.yml|/srv/xg2g/docker-compose.gpu.yml|0644"
)

VERIFIER_SPECS=(
  "docs/ops/xg2g-verifier.service|/srv/xg2g/docs/ops/xg2g-verifier.service|0644"
  "docs/ops/xg2g-verifier.timer|/srv/xg2g/docs/ops/xg2g-verifier.timer|0644"
  "backend/scripts/verify-runtime.sh|/srv/xg2g/scripts/verify-runtime.sh|0755"
  "backend/VERSION|/srv/xg2g/VERSION|0644"
  "DIGESTS.lock|/srv/xg2g/DIGESTS.lock|0644"
  "docs/ops/xg2g-verifier.service|/etc/systemd/system/xg2g-verifier.service|0644"
  "docs/ops/xg2g-verifier.timer|/etc/systemd/system/xg2g-verifier.timer|0644"
)

VERIFIER_TARGETS=(
  "/srv/xg2g/docs/ops/xg2g-verifier.service"
  "/srv/xg2g/docs/ops/xg2g-verifier.timer"
  "/srv/xg2g/scripts/verify-runtime.sh"
  "/srv/xg2g/VERSION"
  "/srv/xg2g/DIGESTS.lock"
  "/etc/systemd/system/xg2g-verifier.service"
  "/etc/systemd/system/xg2g-verifier.timer"
)

usage() {
  cat <<'EOF'
Usage:
  deploy/sync.sh --check [--ref <tag|sha>] [--install-root <path>] [--gpu-overlay auto|enable|disable] [--verifier-bundle auto|enable|disable]
  deploy/sync.sh --apply --ref <tag|sha> [--install-root <path>] [--gpu-overlay auto|enable|disable] [--verifier-bundle auto|enable|disable]

Modes:
  --check   Compare repo truth (current checkout or pinned ref) against the host install root.
  --apply   Copy repo truth for a pinned ref into the host install root, reload systemd, then re-run --check.

Exit codes:
  0  host matches repo truth and env contract is valid
  1  drift detected, but env contract is otherwise valid
  2  env contract violation detected (takes precedence over drift)

Options:
  --ref <tag|sha>         Pinned git ref to archive from the local repo. Required for --apply.
  --install-root <path>   Prefix host targets with a test root. Defaults to /.
  --gpu-overlay <mode>    auto (preserve host intent), enable, or disable.
  --verifier-bundle <mode>
                          auto (preserve host intent), enable, or disable.
  --repo-root <path>      Override repo root. Intended for tests only.
  --fetch-remote <name>   Remote used when --ref is not present locally. Defaults to origin.
  --help                  Show this help.
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

normalize_install_root() {
  local root="$1"
  if [[ "${root}" == "/" ]]; then
    printf '/\n'
    return 0
  fi
  printf '%s\n' "${root%/}"
}

host_path() {
  local absolute_path="$1"
  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    printf '%s\n' "${absolute_path}"
  else
    printf '%s%s\n' "${INSTALL_ROOT}" "${absolute_path}"
  fi
}

require_tool() {
  command -v "$1" >/dev/null 2>&1 || fail "required tool not found: $1"
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
      --ref)
        [[ "$#" -ge 2 ]] || fail "--ref requires a value"
        SOURCE_REF="$2"
        shift 2
        ;;
      --install-root)
        [[ "$#" -ge 2 ]] || fail "--install-root requires a value"
        INSTALL_ROOT="$(normalize_install_root "$2")"
        shift 2
        ;;
      --gpu-overlay)
        [[ "$#" -ge 2 ]] || fail "--gpu-overlay requires a value"
        case "$2" in
          auto|enable|disable) GPU_OVERLAY_MODE="$2" ;;
          *) fail "invalid --gpu-overlay mode: $2" ;;
        esac
        shift 2
        ;;
      --verifier-bundle)
        [[ "$#" -ge 2 ]] || fail "--verifier-bundle requires a value"
        case "$2" in
          auto|enable|disable) VERIFIER_BUNDLE_MODE="$2" ;;
          *) fail "invalid --verifier-bundle mode: $2" ;;
        esac
        shift 2
        ;;
      --repo-root)
        [[ "$#" -ge 2 ]] || fail "--repo-root requires a value"
        REPO_ROOT="$2"
        shift 2
        ;;
      --fetch-remote)
        [[ "$#" -ge 2 ]] || fail "--fetch-remote requires a value"
        FETCH_REMOTE="$2"
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
  if [[ "${MODE}" == "apply" && -z "${SOURCE_REF}" ]]; then
    fail "--apply requires --ref <tag|sha> to avoid implicit checkout state"
  fi
}

resolve_source_tree() {
  local commit archive_root

  if [[ -z "${SOURCE_REF}" ]]; then
    SOURCE_ROOT="${REPO_ROOT}"
    SOURCE_LABEL="working tree"
    return 0
  fi

  commit="$(git -C "${REPO_ROOT}" rev-parse --verify --quiet "${SOURCE_REF}^{commit}" 2>/dev/null || true)"
  if [[ -z "${commit}" ]]; then
    note "git ref ${SOURCE_REF} not present locally; fetching ${FETCH_REMOTE}"
    git -C "${REPO_ROOT}" fetch --tags "${FETCH_REMOTE}"
    commit="$(git -C "${REPO_ROOT}" rev-parse --verify --quiet "${SOURCE_REF}^{commit}" 2>/dev/null || true)"
  fi

  [[ -n "${commit}" ]] || fail "unable to resolve git ref: ${SOURCE_REF}"

  archive_root="$(mktemp -d)"
  TEMP_DIRS+=("${archive_root}")
  git -C "${REPO_ROOT}" archive "${commit}" | tar -x -C "${archive_root}"

  SOURCE_ROOT="${archive_root}"
  SOURCE_COMMIT="${commit}"
  SOURCE_LABEL="${SOURCE_REF} ($(git -C "${REPO_ROOT}" rev-parse --short "${commit}"))"
}

read_env_value() {
  local env_file="$1"
  local key="$2"

  [[ -f "${env_file}" ]] || return 1

  python3 - "${env_file}" "${key}" <<'PY'
import re
import sys

path, wanted = sys.argv[1], sys.argv[2]
assign_re = re.compile(r'^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$')

with open(path, 'r', encoding='utf-8') as fh:
    for raw in fh:
        stripped = raw.strip()
        if not stripped or stripped.startswith('#'):
            continue
        match = assign_re.match(stripped)
        if not match:
            continue
        key, value = match.groups()
        if key != wanted:
            continue
        value = value.strip()
        if value and value[0] in ("'", '"') and value[-1:] == value[0]:
            value = value[1:-1]
        else:
            value = re.sub(r'\s+#.*$', '', value).rstrip()
        print(value)
        sys.exit(0)
sys.exit(1)
PY
}

gpu_overlay_enabled() {
  local env_file compose_file

  case "${GPU_OVERLAY_MODE}" in
    enable)
      return 0
      ;;
    disable)
      return 1
      ;;
    auto)
      if [[ ! -f "${SOURCE_ROOT}/deploy/docker-compose.gpu.yml" ]]; then
        return 1
      fi

      env_file="$(host_path "/etc/xg2g/xg2g.env")"
      compose_file="$(read_env_value "${env_file}" COMPOSE_FILE 2>/dev/null || true)"
      if [[ "${compose_file}" == *"docker-compose.gpu.yml"* ]]; then
        return 0
      fi

      [[ -e "$(host_path "/srv/xg2g/docker-compose.gpu.yml")" ]]
      ;;
  esac
}

verifier_bundle_enabled() {
  local target

  case "${VERIFIER_BUNDLE_MODE}" in
    enable)
      return 0
      ;;
    disable)
      return 1
      ;;
    auto)
      for target in "${VERIFIER_TARGETS[@]}"; do
        if [[ -e "$(host_path "${target}")" ]]; then
          return 0
        fi
      done
      return 1
      ;;
  esac
}

build_manifest() {
  CURRENT_SPECS=("${CORE_SPECS[@]}")
  GPU_OVERLAY_ENABLED=0
  VERIFIER_BUNDLE_ENABLED=0

  if gpu_overlay_enabled; then
    GPU_OVERLAY_ENABLED=1
    CURRENT_SPECS+=("${GPU_SPECS[@]}")
  fi

  if verifier_bundle_enabled; then
    VERIFIER_BUNDLE_ENABLED=1
    CURRENT_SPECS+=("${VERIFIER_SPECS[@]}")
  fi
}

compare_spec() {
  local spec="$1"
  local rel target expected_mode
  local source_file target_file actual_mode

  IFS='|' read -r rel target expected_mode <<< "${spec}"
  source_file="${SOURCE_ROOT}/${rel}"
  target_file="$(host_path "${target}")"

  [[ -f "${source_file}" ]] || fail "missing source artifact in ${SOURCE_LABEL}: ${rel}"

  if [[ ! -f "${target_file}" ]]; then
    DRIFT_DETECTED=1
    warn "missing target file: ${target_file} (expected from ${rel})"
    diff -u --label "${SOURCE_LABEL}:${rel}" --label "${target_file}" "${source_file}" /dev/null || true
    return 0
  fi

  if ! diff -u --label "${SOURCE_LABEL}:${rel}" --label "${target_file}" "${source_file}" "${target_file}" >/dev/null; then
    DRIFT_DETECTED=1
    warn "content drift: ${target_file}"
    diff -u --label "${SOURCE_LABEL}:${rel}" --label "${target_file}" "${source_file}" "${target_file}" || true
  fi

  actual_mode="$(stat -c '%a' "${target_file}")"
  if [[ "${actual_mode}" != "${expected_mode}" ]]; then
    DRIFT_DETECTED=1
    warn "mode drift: ${target_file} expected ${expected_mode}, got ${actual_mode}"
  fi
}

compare_absent_target() {
  local target="$1"
  local target_file

  target_file="$(host_path "${target}")"
  if [[ -e "${target_file}" ]]; then
    DRIFT_DETECTED=1
    warn "unexpected target present: ${target_file}"
    diff -u --label /dev/null --label "${target_file}" /dev/null "${target_file}" || true
  fi
}

validate_env_contract() {
  local schema_file env_file output status line

  schema_file="${SOURCE_ROOT}/deploy/xg2g.env.schema.yaml"
  env_file="$(host_path "/etc/xg2g/xg2g.env")"

  [[ -f "${schema_file}" ]] || fail "missing env schema in ${SOURCE_LABEL}: deploy/xg2g.env.schema.yaml"

  set +e
  output="$(python3 - "${schema_file}" "${env_file}" <<'PY'
import re
import sys

schema_path, env_path = sys.argv[1], sys.argv[2]

unknown_keys_mode = "warn"
fields = {}
validations = []
current_key = None
current_rule = None
collect_rule_keys = False

scalar_re = re.compile(r'^([A-Za-z0-9_]+):\s*(.*)$')
key_re = re.compile(r'^ {6}([A-Z0-9_]+):\s*$')
key_attr_re = re.compile(r'^ {8}([A-Za-z0-9_]+):\s*(.*)$')
rule_re = re.compile(r'^  - id:\s*(\S+)\s*$')
rule_attr_re = re.compile(r'^    ([A-Za-z0-9_]+):\s*(.*)$')
rule_list_item_re = re.compile(r'^      -\s*([A-Z0-9_]+)\s*$')

def parse_scalar(value):
    value = value.strip()
    if value.startswith('"') and value.endswith('"') and len(value) >= 2:
        return value[1:-1]
    if value.startswith("'") and value.endswith("'") and len(value) >= 2:
        return value[1:-1]
    return value

with open(schema_path, 'r', encoding='utf-8') as fh:
    for raw in fh:
        line = raw.rstrip('\n')
        if not line.strip() or line.lstrip().startswith('#'):
            continue

        if line.startswith('unknownKeys:'):
            unknown_keys_mode = parse_scalar(line.split(':', 1)[1])
            continue

        match = key_re.match(line)
        if match:
            current_key = match.group(1)
            fields.setdefault(current_key, {})
            continue

        if current_key is not None:
            match = key_attr_re.match(line)
            if match:
                attr, value = match.groups()
                fields[current_key][attr] = parse_scalar(value)
                continue
            if re.match(r'^ {6}\S', line) or re.match(r'^ {2}\S', line):
                current_key = None

        match = rule_re.match(line)
        if match:
            current_rule = {"id": match.group(1), "keys": []}
            validations.append(current_rule)
            collect_rule_keys = False
            continue

        if current_rule is not None:
            if line == '    keys:':
                collect_rule_keys = True
                current_rule["keys"] = []
                continue

            match = rule_list_item_re.match(line)
            if collect_rule_keys and match:
                current_rule["keys"].append(match.group(1))
                continue

            match = rule_attr_re.match(line)
            if match:
                attr, value = match.groups()
                current_rule[attr] = parse_scalar(value)
                if attr != "keys":
                    collect_rule_keys = False
                continue

            if re.match(r'^  - id:\s*', line) or re.match(r'^\S', line):
                current_rule = None
                collect_rule_keys = False

env = {}
errors = []
warnings = []
assign_re = re.compile(r'^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=(.*)$')

try:
    with open(env_path, 'r', encoding='utf-8') as fh:
        for lineno, raw in enumerate(fh, start=1):
            stripped = raw.strip()
            if not stripped or stripped.startswith('#'):
                continue

            match = assign_re.match(stripped)
            if not match:
                errors.append(f"invalid env line {lineno}: expected KEY=VALUE syntax")
                continue

            key, value = match.groups()
            value = value.strip()
            if value and value[0] in ("'", '"') and value[-1:] == value[0]:
                value = value[1:-1]
            else:
                value = re.sub(r'\s+#.*$', '', value).rstrip()
            env[key] = value
except FileNotFoundError:
    errors.append(f"missing env file: {env_path}")

def trimmed(key):
    return env.get(key, "").strip()

for key, attrs in fields.items():
    if attrs.get("required", "false") == "true" and not trimmed(key):
        errors.append(f"missing required env key: {key}")

if unknown_keys_mode == "warn":
    for key in sorted(env):
        if key not in fields:
            warnings.append(f"unknown env key outside deploy schema: {key}")

for rule in validations:
    kind = rule.get("kind")
    if kind == "at_least_one_present":
        keys = rule.get("keys", [])
        if not any(trimmed(key) for key in keys):
            errors.append(f"validation {rule['id']} failed: need at least one of {', '.join(keys)}")
    elif kind == "equal_trimmed_when_present":
        keys = rule.get("keys", [])
        if len(keys) == 2 and trimmed(keys[0]) and trimmed(keys[1]) and trimmed(keys[0]) != trimmed(keys[1]):
            errors.append(f"validation {rule['id']} failed: {keys[0]} and {keys[1]} differ")
    elif kind == "non_empty":
        key = rule.get("key")
        if key and not trimmed(key):
            errors.append(f"validation {rule['id']} failed: {key} must be non-empty")
    elif kind == "min_length":
        key = rule.get("key")
        min_len = int(rule.get("min", "0"))
        if key and len(trimmed(key)) < min_len:
            errors.append(f"validation {rule['id']} failed: {key} must be at least {min_len} bytes after trimming")

for warning in warnings:
    print(f"WARN\t{warning}")

for error in errors:
    print(f"ERROR\t{error}")

sys.exit(2 if errors else 0)
PY
  )"
  status=$?
  set -e

  if [[ -n "${output}" ]]; then
    while IFS= read -r line; do
      [[ -n "${line}" ]] || continue
      case "${line}" in
        WARN$'\t'*)
          warn "${line#*$'\t'}"
          ;;
        ERROR$'\t'*)
          warn "${line#*$'\t'}"
          ;;
        *)
          warn "${line}"
          ;;
      esac
    done <<< "${output}"
  fi

  return "${status}"
}

copy_spec() {
  local spec="$1"
  local rel target mode
  local source_file target_file

  IFS='|' read -r rel target mode <<< "${spec}"
  source_file="${SOURCE_ROOT}/${rel}"
  target_file="$(host_path "${target}")"

  [[ -f "${source_file}" ]] || fail "missing source artifact in ${SOURCE_LABEL}: ${rel}"
  install -d "$(dirname "${target_file}")"
  install -m "${mode}" "${source_file}" "${target_file}"
}

remove_target_if_present() {
  local absolute_target="$1"
  local target_file

  target_file="$(host_path "${absolute_target}")"
  if [[ -e "${target_file}" ]]; then
    rm -f "${target_file}"
    note "removed ${target_file}"
  fi
}

remove_disabled_optionals() {
  local spec rel target mode

  if [[ "${GPU_OVERLAY_ENABLED}" -eq 0 ]]; then
    for spec in "${GPU_SPECS[@]}"; do
      IFS='|' read -r rel target mode <<< "${spec}"
      remove_target_if_present "${target}"
    done
  fi

  if [[ "${VERIFIER_BUNDLE_ENABLED}" -eq 0 ]]; then
    for spec in "${VERIFIER_SPECS[@]}"; do
      IFS='|' read -r rel target mode <<< "${spec}"
      remove_target_if_present "${target}"
    done
  fi
}

run_check() {
  local contract_status=0
  local spec rel target mode

  DRIFT_DETECTED=0
  build_manifest

  for spec in "${CURRENT_SPECS[@]}"; do
    compare_spec "${spec}"
  done

  if [[ "${GPU_OVERLAY_ENABLED}" -eq 0 ]]; then
    for spec in "${GPU_SPECS[@]}"; do
      IFS='|' read -r rel target mode <<< "${spec}"
      compare_absent_target "${target}"
    done
  fi

  if [[ "${VERIFIER_BUNDLE_ENABLED}" -eq 0 ]]; then
    for spec in "${VERIFIER_SPECS[@]}"; do
      IFS='|' read -r rel target mode <<< "${spec}"
      compare_absent_target "${target}"
    done
  fi

  if ! validate_env_contract; then
    contract_status=2
  fi

  if [[ "${contract_status}" -eq 2 ]]; then
    return 2
  fi

  if [[ "${DRIFT_DETECTED}" -eq 1 ]]; then
    return 1
  fi

  return 0
}

apply_sync() {
  local spec status

  build_manifest

  for spec in "${CURRENT_SPECS[@]}"; do
    copy_spec "${spec}"
  done

  remove_disabled_optionals

  if [[ "${INSTALL_ROOT}" == "/" ]]; then
    require_tool systemctl
    systemctl daemon-reload
  else
    note "skipping systemctl daemon-reload for install root ${INSTALL_ROOT}"
  fi

  set +e
  run_check
  status=$?
  set -e

  if [[ "${status}" -ne 0 ]]; then
    warn "apply completed, but post-apply verification failed"
  fi

  return "${status}"
}

main() {
  local status

  parse_args "$@"

  require_tool git
  require_tool tar
  require_tool install
  require_tool diff
  require_tool stat
  require_tool python3

  resolve_source_tree
  note "using source ${SOURCE_LABEL}"

  if [[ "${MODE}" == "check" ]]; then
    set +e
    run_check
    status=$?
    set -e
    exit "${status}"
  fi

  if [[ "${INSTALL_ROOT}" == "/" && "${EUID}" -ne 0 ]]; then
    fail "--apply against / requires root"
  fi

  apply_sync
}

main "$@"
