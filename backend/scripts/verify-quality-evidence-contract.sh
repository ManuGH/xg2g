#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
coverage_workflow="${repo_root}/.github/workflows/coverage.yml"
container_workflow="${repo_root}/.github/workflows/container-security.yml"
govuln_workflow="${repo_root}/.github/workflows/govulncheck.yml"
quality_make="${repo_root}/mk/quality.mk"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

threshold="$(make --no-print-directory -s -C "${repo_root}" print-coverage-threshold)"
[[ "${threshold}" =~ ^[0-9]+([.][0-9]+)?$ ]] ||
  fail "coverage threshold must be numeric"
awk -v threshold="${threshold}" 'BEGIN { exit !((threshold + 0) >= 70) }' ||
  fail "coverage threshold must remain at or above the reviewed 70% baseline"

grep -Fq "scripts/ci/enforce-coverage.sh" "${coverage_workflow}" ||
  fail "Coverage workflow must use the shared fail-closed coverage gate"
grep -Fq "scripts/ci/enforce-coverage.sh" "${quality_make}" ||
  fail "Local test-cover must use the shared fail-closed coverage gate"
if grep -Fq "vars.COVERAGE_MIN" "${coverage_workflow}"; then
  fail "Coverage enforcement must not depend on an optional repository variable"
fi

upload_count="$(
  grep -Fc "github/codeql-action/upload-sarif@" "${container_workflow}" || true
)"
[[ "${upload_count}" -ge 2 ]] ||
  fail "Container Security must publish both image and filesystem SARIF evidence"
grep -Fq "sarif_file: trivy-results.sarif" "${container_workflow}" ||
  fail "production image SARIF is not uploaded"
grep -Fq "category: release-container-image" "${container_workflow}" ||
  fail "production image SARIF category is missing"
grep -Fq "sarif_file: trivy-fs-results.sarif" "${container_workflow}" ||
  fail "release filesystem SARIF is not uploaded"
grep -Fq "category: release-filesystem" "${container_workflow}" ||
  fail "release filesystem SARIF category is missing"
image_build_count="$(
  grep -Ec 'name: "?Build production image' "${container_workflow}" || true
)"
[[ "${image_build_count}" -eq 1 ]] ||
  fail "Container Security must build the production image exactly once"
grep -Fq "Generate SBOM for production image" "${container_workflow}" ||
  fail "the single production image must also produce the release SBOM"

if grep -A4 -F "Upload SARIF to code scanning" "${govuln_workflow}" | grep -Fq "success()"; then
  fail "govulncheck SARIF upload must run even when an earlier scanner step fails"
fi

echo "OK: coverage and scanner evidence are fail-closed and repository-governed."
