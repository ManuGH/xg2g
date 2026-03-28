#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

UNIT_TEMPLATE="${REPO_ROOT}/backend/templates/docs/ops/xg2g.service.tmpl"
CANONICAL_UNIT="${REPO_ROOT}/docs/ops/xg2g.service"
RUNBOOK="${REPO_ROOT}/docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md"
COMPOSE_HELPER="${REPO_ROOT}/backend/scripts/compose-xg2g.sh"
COMPOSE_CONTRACT="${REPO_ROOT}/backend/scripts/verify-compose-contract.sh"

CANONICAL_ROOT="/srv/xg2g"
CANONICAL_ENV_FILE="/etc/xg2g/xg2g.env"
CANONICAL_HELPER="${CANONICAL_ROOT}/scripts/compose-xg2g.sh"
CANONICAL_COMPOSE_CONTRACT="${CANONICAL_ROOT}/scripts/verify-compose-contract.sh"
CANONICAL_UNIT_HEADER="# GENERATED FILE - DO NOT EDIT. Source: backend/templates/docs/ops/xg2g.service.tmpl"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

assert_file() {
  local file="$1"

  [[ -f "${file}" ]] || fail "missing required file: ${file}"
}

assert_exact_line() {
  local file="$1"
  local line="$2"
  local label="$3"

  grep -Fqx "${line}" "${file}" || fail "${label}: expected line '${line}' in ${file}"
}

assert_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"

  grep -Fq "${needle}" "${file}" || fail "${label}: expected '${needle}' in ${file}"
}

assert_regex() {
  local file="$1"
  local pattern="$2"
  local label="$3"

  grep -Eq "${pattern}" "${file}" || fail "${label}: expected pattern '${pattern}' in ${file}"
}

verify_unit_render_sync() {
  assert_file "${UNIT_TEMPLATE}"
  assert_file "${CANONICAL_UNIT}"
  assert_exact_line "${CANONICAL_UNIT}" "${CANONICAL_UNIT_HEADER}" "generated unit header"

  local rendered_body
  rendered_body="$(mktemp)"

  tail -n +2 "${CANONICAL_UNIT}" > "${rendered_body}"
  diff -u "${UNIT_TEMPLATE}" "${rendered_body}" >/dev/null || fail "docs/ops/xg2g.service drifted from backend/templates/docs/ops/xg2g.service.tmpl"
  rm -f "${rendered_body}"
}

verify_unit_semantics() {
  local unit_file="$1"

  assert_file "${unit_file}"

  assert_exact_line "${unit_file}" "ConditionPathIsDirectory=${CANONICAL_ROOT}" "unit working directory condition"
  assert_exact_line "${unit_file}" "ConditionPathExists=${CANONICAL_ROOT}/docker-compose.yml" "unit base compose condition"
  assert_exact_line "${unit_file}" "ConditionPathIsDirectory=/var/lib/xg2g" "unit data dir condition"
  assert_exact_line "${unit_file}" "Type=oneshot" "unit type"
  assert_exact_line "${unit_file}" "RemainAfterExit=yes" "unit remain-after-exit"
  assert_exact_line "${unit_file}" "WorkingDirectory=${CANONICAL_ROOT}" "unit working directory"
  assert_exact_line "${unit_file}" "EnvironmentFile=${CANONICAL_ENV_FILE}" "unit environment file"
  assert_exact_line "${unit_file}" "ExecStartPre=${CANONICAL_HELPER} config -q" "unit compose preflight"
  assert_exact_line "${unit_file}" "ExecStartPre=${CANONICAL_COMPOSE_CONTRACT}" "unit compose contract preflight"
  assert_exact_line "${unit_file}" "ExecStart=${CANONICAL_HELPER} up -d --remove-orphans" "unit start path"
  assert_exact_line "${unit_file}" "ExecStop=${CANONICAL_HELPER} stop" "unit stop path"
  assert_exact_line "${unit_file}" "ExecReload=${CANONICAL_HELPER} up -d --remove-orphans" "unit reload path"
  assert_exact_line "${unit_file}" "TimeoutStartSec=180" "unit start timeout"
  assert_exact_line "${unit_file}" "TimeoutStopSec=60" "unit stop timeout"
  assert_exact_line "${unit_file}" "UMask=0077" "unit umask"
  assert_exact_line "${unit_file}" "NoNewPrivileges=true" "unit no-new-privileges"

  assert_contains "${unit_file}" "XG2G_E2_HOST" "unit required env preflight"
  assert_contains "${unit_file}" "XG2G_API_TOKEN" "unit API token preflight"
  assert_contains "${unit_file}" "XG2G_DECISION_SECRET" "unit decision secret preflight"
  assert_contains "${unit_file}" "need >= 32 bytes for HS256" "unit decision secret length guard"
  assert_contains "${unit_file}" "${CANONICAL_HELPER} ps -q xg2g" "unit start-post health helper"
  assert_contains "${unit_file}" "docker inspect --format \"{{.State.Health.Status}}\"" "unit health poll"

  assert_regex "${unit_file}" '^ExecStartPost=.*Timed out waiting for healthy' "unit start-post timeout gate"
  assert_regex "${unit_file}" '^ExecStartPre=.*awk .* /srv/xg2g/docker-compose\.yml' "unit service-name invariant"

  if grep -Eq '^Exec(Start|Stop|Reload|StartPost)=.*docker compose' "${unit_file}"; then
    fail "unit runtime actions must use ${CANONICAL_HELPER}, not direct docker compose"
  fi
}

verify_helper_semantics() {
  assert_file "${COMPOSE_HELPER}"
  assert_file "${COMPOSE_CONTRACT}"

  assert_exact_line "${COMPOSE_HELPER}" "ROOT=\"\${XG2G_COMPOSE_ROOT:-${CANONICAL_ROOT}}\"" "compose helper root default"
  assert_exact_line "${COMPOSE_HELPER}" "ENV_FILE=\"\${XG2G_ENV_FILE:-${CANONICAL_ENV_FILE}}\"" "compose helper env default"
  assert_exact_line "${COMPOSE_HELPER}" "BASE_FILE=\"\${ROOT}/docker-compose.yml\"" "compose helper base compose"
  assert_exact_line "${COMPOSE_HELPER}" "GPU_FILE=\"\${ROOT}/docker-compose.gpu.yml\"" "compose helper gpu overlay"
  assert_contains "${COMPOSE_HELPER}" "COMPOSE_FILE" "compose helper COMPOSE_FILE support"

  assert_exact_line "${COMPOSE_CONTRACT}" "ROOT=\"${CANONICAL_ROOT}\"" "compose contract root"
  assert_exact_line "${COMPOSE_CONTRACT}" "COMPOSE_HELPER=\"\$ROOT/scripts/compose-xg2g.sh\"" "compose contract helper path"
}

verify_runbook_semantics() {
  assert_file "${RUNBOOK}"

  assert_contains "${RUNBOOK}" "Base compose file path is frozen to \`${CANONICAL_ROOT}/docker-compose.yml\`." "runbook base compose invariant"
  assert_contains "${RUNBOOK}" "Optional GPU overlay path is \`${CANONICAL_ROOT}/docker-compose.gpu.yml\`." "runbook gpu overlay invariant"
  assert_contains "${RUNBOOK}" "Working directory must be \`${CANONICAL_ROOT}\`." "runbook working directory invariant"
  assert_contains "${RUNBOOK}" "Use \`${CANONICAL_HELPER}\` for manual compose operations" "runbook helper invariant"
  assert_contains "${RUNBOOK}" "\`${CANONICAL_ENV_FILE}\` must be \`root:root\` and \`0600\`" "runbook env permissions"
  assert_contains "${RUNBOOK}" "${CANONICAL_ROOT}/scripts/verify-systemd-runtime-contract.sh" "runbook runtime contract verifier"
  assert_contains "${RUNBOOK}" "XG2G_E2_HOST" "runbook required env host"
  assert_contains "${RUNBOOK}" "XG2G_API_TOKEN" "runbook required env token"
  assert_contains "${RUNBOOK}" "XG2G_DECISION_SECRET" "runbook required env decision secret"
}

verify_negative_drift_guard() {
  local bad_unit
  bad_unit="$(mktemp)"

  cp "${CANONICAL_UNIT}" "${bad_unit}"
  perl -0pi -e 's#EnvironmentFile=/etc/xg2g/xg2g\.env#EnvironmentFile=/tmp/xg2g.env#' "${bad_unit}"

  if "${BASH_SOURCE[0]}" --verify-unit "${bad_unit}" >/dev/null 2>&1; then
    fail "negative drift guard failed: mutated unit unexpectedly passed"
  fi

  rm -f "${bad_unit}"
}

main() {
  case "${1:-}" in
    --verify-unit)
      [[ "$#" -eq 2 ]] || fail "usage: $0 --verify-unit <unit-file>"
      verify_unit_semantics "$2"
      return 0
      ;;
    "")
      ;;
    *)
      fail "usage: $0 [--verify-unit <unit-file>]"
      ;;
  esac

  verify_unit_render_sync
  verify_unit_semantics "${CANONICAL_UNIT}"
  verify_helper_semantics
  verify_runbook_semantics
  verify_negative_drift_guard

  echo "OK: systemd runtime contract holds."
}

main "$@"
