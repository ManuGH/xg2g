#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

INSTALL_DOC="${REPO_ROOT}/docs/ops/INSTALLATION_CONTRACT.md"
DEPLOYMENT_INDEX="${REPO_ROOT}/docs/ops/DEPLOYMENT.md"
RUNBOOK="${REPO_ROOT}/docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md"

REQUIRED_REPO_FILES=(
  "docker-compose.yml"
  "docs/ops/xg2g.service"
  "backend/scripts/compose-xg2g.sh"
  "backend/scripts/verify-compose-contract.sh"
  "backend/scripts/verify-installed-unit.sh"
  "backend/scripts/verify-systemd-runtime-contract.sh"
)

REQUIRED_REPO_EXECUTABLES=(
  "backend/scripts/compose-xg2g.sh"
  "backend/scripts/verify-compose-contract.sh"
  "backend/scripts/verify-installed-unit.sh"
  "backend/scripts/verify-systemd-runtime-contract.sh"
  "backend/scripts/verify-installation-contract.sh"
)

OPTIONAL_REPO_FILES=(
  "docker-compose.gpu.yml"
  "docs/ops/xg2g-verifier.service"
  "docs/ops/xg2g-verifier.timer"
  "backend/scripts/verify-runtime.sh"
  "backend/VERSION"
  "DIGESTS.lock"
)

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

join_path() {
  local root="$1"
  local target="$2"
  printf '%s%s\n' "${root%/}" "${target}"
}

assert_file() {
  local path="$1"
  [[ -f "${path}" ]] || fail "missing required file: ${path}"
}

assert_dir() {
  local path="$1"
  [[ -d "${path}" ]] || fail "missing required directory: ${path}"
}

assert_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  grep -Fq -- "${needle}" "${file}" || fail "${label}: expected '${needle}' in ${file}"
}

assert_mode() {
  local path="$1"
  local expected="$2"
  local label="$3"
  local actual
  actual="$(stat -c '%a' "${path}")"
  [[ "${actual}" == "${expected}" ]] || fail "${label}: expected mode ${expected}, got ${actual} for ${path}"
}

assert_executable() {
  local path="$1"
  local label="$2"
  [[ -x "${path}" ]] || fail "${label}: expected executable file at ${path}"
}

assert_same_file() {
  local left="$1"
  local right="$2"
  local label="$3"
  diff -u "${left}" "${right}" >/dev/null || fail "${label}: ${left} and ${right} differ"
}

verify_installation_doc() {
  assert_file "${INSTALL_DOC}"
  assert_contains "${INSTALL_DOC}" "## Required Host Layout" "installation contract section"
  assert_contains "${INSTALL_DOC}" '`/srv/xg2g/docker-compose.yml`' "installation contract base compose"
  assert_contains "${INSTALL_DOC}" '`/srv/xg2g/scripts/compose-xg2g.sh`' "installation contract compose helper"
  assert_contains "${INSTALL_DOC}" '`/srv/xg2g/scripts/verify-installation-contract.sh`' "installation contract verifier script"
  assert_contains "${INSTALL_DOC}" '`/etc/systemd/system/xg2g.service`' "installation contract installed unit"
  assert_contains "${INSTALL_DOC}" '`/etc/xg2g/xg2g.env`' "installation contract env file"
  assert_contains "${INSTALL_DOC}" '`/var/lib/xg2g`' "installation contract data dir"
  assert_contains "${INSTALL_DOC}" "## Optional Periodic Verifier Bundle" "installation contract optional verifier bundle"
  assert_contains "${INSTALL_DOC}" "all-or-nothing" "installation contract bundle rule"
  assert_contains "${INSTALL_DOC}" "--verify-install-root /" "installation contract live-host verification"
}

verify_docs_discoverability() {
  assert_file "${DEPLOYMENT_INDEX}"
  assert_contains "${DEPLOYMENT_INDEX}" '`docs/ops/INSTALLATION_CONTRACT.md`' "deployment index installation contract"

  assert_file "${RUNBOOK}"
  assert_contains "${RUNBOOK}" 'Canonical install layout: `docs/ops/INSTALLATION_CONTRACT.md`.' "runbook installation contract link"
  assert_contains "${RUNBOOK}" "/srv/xg2g/scripts/verify-installation-contract.sh --verify-install-root /" "runbook installation verifier command"
}

verify_repo_sources() {
  local path

  for path in "${REQUIRED_REPO_FILES[@]}"; do
    assert_file "${REPO_ROOT}/${path}"
  done

  for path in "${OPTIONAL_REPO_FILES[@]}"; do
    assert_file "${REPO_ROOT}/${path}"
  done

  for path in "${REQUIRED_REPO_EXECUTABLES[@]}"; do
    assert_executable "${REPO_ROOT}/${path}" "repo executable"
  done
}

verify_install_tree() {
  local root="$1"

  local srv_root
  local etc_root
  local systemd_root
  local data_root
  srv_root="$(join_path "${root}" "/srv/xg2g")"
  etc_root="$(join_path "${root}" "/etc/xg2g")"
  systemd_root="$(join_path "${root}" "/etc/systemd/system")"
  data_root="$(join_path "${root}" "/var/lib/xg2g")"

  assert_dir "${srv_root}"
  assert_dir "${srv_root}/scripts"
  assert_dir "${srv_root}/docs/ops"
  assert_dir "${etc_root}"
  assert_dir "${systemd_root}"
  assert_dir "${data_root}"

  assert_file "${srv_root}/docker-compose.yml"
  assert_mode "${srv_root}/docker-compose.yml" "644" "base compose mode"

  assert_file "${srv_root}/docs/ops/xg2g.service"
  assert_mode "${srv_root}/docs/ops/xg2g.service" "644" "canonical unit mode"

  assert_file "${srv_root}/scripts/compose-xg2g.sh"
  assert_mode "${srv_root}/scripts/compose-xg2g.sh" "755" "compose helper mode"
  assert_executable "${srv_root}/scripts/compose-xg2g.sh" "compose helper executable"

  assert_file "${srv_root}/scripts/verify-compose-contract.sh"
  assert_mode "${srv_root}/scripts/verify-compose-contract.sh" "755" "compose contract script mode"
  assert_executable "${srv_root}/scripts/verify-compose-contract.sh" "compose contract script executable"

  assert_file "${srv_root}/scripts/verify-installed-unit.sh"
  assert_mode "${srv_root}/scripts/verify-installed-unit.sh" "755" "installed-unit verifier mode"
  assert_executable "${srv_root}/scripts/verify-installed-unit.sh" "installed-unit verifier executable"

  assert_file "${srv_root}/scripts/verify-systemd-runtime-contract.sh"
  assert_mode "${srv_root}/scripts/verify-systemd-runtime-contract.sh" "755" "systemd runtime verifier mode"
  assert_executable "${srv_root}/scripts/verify-systemd-runtime-contract.sh" "systemd runtime verifier executable"

  assert_file "${srv_root}/scripts/verify-installation-contract.sh"
  assert_mode "${srv_root}/scripts/verify-installation-contract.sh" "755" "installation verifier mode"
  assert_executable "${srv_root}/scripts/verify-installation-contract.sh" "installation verifier executable"

  assert_file "${systemd_root}/xg2g.service"
  assert_mode "${systemd_root}/xg2g.service" "644" "installed unit mode"
  assert_same_file "${srv_root}/docs/ops/xg2g.service" "${systemd_root}/xg2g.service" "installed unit parity"

  assert_file "${etc_root}/xg2g.env"
  assert_mode "${etc_root}/xg2g.env" "600" "env file mode"

  if [[ -e "${srv_root}/docker-compose.gpu.yml" ]]; then
    assert_mode "${srv_root}/docker-compose.gpu.yml" "644" "gpu overlay mode"
  fi

  local verifier_bundle=(
    "${srv_root}/docs/ops/xg2g-verifier.service"
    "${srv_root}/docs/ops/xg2g-verifier.timer"
    "${srv_root}/scripts/verify-runtime.sh"
    "${srv_root}/VERSION"
    "${srv_root}/DIGESTS.lock"
    "${systemd_root}/xg2g-verifier.service"
    "${systemd_root}/xg2g-verifier.timer"
  )
  local verifier_present=0
  local item
  for item in "${verifier_bundle[@]}"; do
    if [[ -e "${item}" ]]; then
      verifier_present=1
      break
    fi
  done

  if [[ "${verifier_present}" -eq 1 ]]; then
    for item in "${verifier_bundle[@]}"; do
      [[ -e "${item}" ]] || fail "optional verifier bundle is partial; missing ${item}"
    done

    assert_mode "${srv_root}/docs/ops/xg2g-verifier.service" "644" "verifier service source mode"
    assert_mode "${srv_root}/docs/ops/xg2g-verifier.timer" "644" "verifier timer source mode"
    assert_mode "${srv_root}/scripts/verify-runtime.sh" "755" "verify-runtime mode"
    assert_executable "${srv_root}/scripts/verify-runtime.sh" "verify-runtime executable"
    assert_mode "${srv_root}/VERSION" "644" "VERSION mode"
    assert_mode "${srv_root}/DIGESTS.lock" "644" "DIGESTS.lock mode"
    assert_mode "${systemd_root}/xg2g-verifier.service" "644" "installed verifier service mode"
    assert_mode "${systemd_root}/xg2g-verifier.timer" "644" "installed verifier timer mode"
    assert_same_file "${srv_root}/docs/ops/xg2g-verifier.service" "${systemd_root}/xg2g-verifier.service" "installed verifier service parity"
    assert_same_file "${srv_root}/docs/ops/xg2g-verifier.timer" "${systemd_root}/xg2g-verifier.timer" "installed verifier timer parity"
  fi
}

build_reference_install_tree() {
  local root="$1"
  local install_root
  install_root="$(join_path "${root}" "")"

  install -d "${install_root}/srv/xg2g/scripts" "${install_root}/srv/xg2g/docs/ops" "${install_root}/etc/systemd/system" "${install_root}/etc/xg2g" "${install_root}/var/lib/xg2g"

  install -m 0644 "${REPO_ROOT}/docker-compose.yml" "${install_root}/srv/xg2g/docker-compose.yml"
  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g.service" "${install_root}/srv/xg2g/docs/ops/xg2g.service"
  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g.service" "${install_root}/etc/systemd/system/xg2g.service"

  install -m 0755 "${REPO_ROOT}/backend/scripts/compose-xg2g.sh" "${install_root}/srv/xg2g/scripts/compose-xg2g.sh"
  install -m 0755 "${REPO_ROOT}/backend/scripts/verify-compose-contract.sh" "${install_root}/srv/xg2g/scripts/verify-compose-contract.sh"
  install -m 0755 "${REPO_ROOT}/backend/scripts/verify-installed-unit.sh" "${install_root}/srv/xg2g/scripts/verify-installed-unit.sh"
  install -m 0755 "${REPO_ROOT}/backend/scripts/verify-systemd-runtime-contract.sh" "${install_root}/srv/xg2g/scripts/verify-systemd-runtime-contract.sh"
  install -m 0755 "${REPO_ROOT}/backend/scripts/verify-installation-contract.sh" "${install_root}/srv/xg2g/scripts/verify-installation-contract.sh"

  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "${install_root}/srv/xg2g/docs/ops/xg2g-verifier.service"
  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "${install_root}/srv/xg2g/docs/ops/xg2g-verifier.timer"
  install -m 0755 "${REPO_ROOT}/backend/scripts/verify-runtime.sh" "${install_root}/srv/xg2g/scripts/verify-runtime.sh"
  install -m 0644 "${REPO_ROOT}/backend/VERSION" "${install_root}/srv/xg2g/VERSION"
  install -m 0644 "${REPO_ROOT}/DIGESTS.lock" "${install_root}/srv/xg2g/DIGESTS.lock"
  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "${install_root}/etc/systemd/system/xg2g-verifier.service"
  install -m 0644 "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "${install_root}/etc/systemd/system/xg2g-verifier.timer"

  printf 'PLACEHOLDER=1\n' > "${install_root}/etc/xg2g/xg2g.env"
  chmod 600 "${install_root}/etc/xg2g/xg2g.env"
}

verify_negative_drift_guard() {
  local root
  root="$(mktemp -d)"

  build_reference_install_tree "${root}"
  verify_install_tree "${root}"

  chmod 644 "$(join_path "${root}" "/etc/xg2g/xg2g.env")"
  if "${BASH_SOURCE[0]}" --verify-install-root "${root}" >/dev/null 2>&1; then
    fail "negative drift guard failed: mis-moded env file unexpectedly passed"
  fi

  rm -rf "${root}"
}

main() {
  case "${1:-}" in
    --verify-install-root)
      [[ "$#" -eq 2 ]] || fail "usage: $0 --verify-install-root <root>"
      verify_install_tree "$2"
      echo "OK: installation contract holds for $2."
      return 0
      ;;
    "")
      ;;
    *)
      fail "usage: $0 [--verify-install-root <root>]"
      ;;
  esac

  verify_installation_doc
  verify_docs_discoverability
  verify_repo_sources
  verify_negative_drift_guard

  echo "OK: installation contract holds."
}

main "$@"
