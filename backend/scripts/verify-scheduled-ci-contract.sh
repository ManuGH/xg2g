#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workflows_dir="${repo_root}/.github/workflows"
deep_workflow="${workflows_dir}/ci-deep-scheduled.yml"
policy="${repo_root}/docs/ops/CI_POLICY.md"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

for legacy in ci-v2.yml ci-nightly.yml; do
  [[ ! -e "${workflows_dir}/${legacy}" ]] ||
    fail "legacy scheduled workflow ${legacy} must not be restored"
done

daily_workflows="$(
  grep -El "cron:[[:space:]]+['\"]?[0-9*/,-]+[[:space:]]+[0-9*/,-]+[[:space:]]+\*[[:space:]]+\*[[:space:]]+\*" \
    "${workflows_dir}"/*.yml || true
)"
daily_count="$(printf '%s\n' "${daily_workflows}" | awk 'NF { count++ } END { print count + 0 }')"
[[ "${daily_count}" -eq 1 ]] ||
  fail "expected exactly one workflow with a daily cron, found ${daily_count}"
[[ "${daily_workflows}" == "${deep_workflow}" ]] ||
  fail "daily cron must be owned by ci-deep-scheduled.yml"

grep -Fq "nightly-performance:" "${deep_workflow}" ||
  fail "deep workflow is missing the nightly-performance job"
grep -Fq "make performance-gate" "${deep_workflow}" ||
  fail "deep workflow does not execute the performance gate"
grep -Fq "nightly race, integration, performance budgets, and spec/doc lint" "${policy}" ||
  fail "CI policy does not describe the consolidated nightly suite"

echo "OK: scheduled CI has one nightly owner and an enforced performance gate."
