#!/usr/bin/env bash
# Assert that the aggregate gate job depends on every other job in its workflow.
#
# Why: `CI / Gate` evaluates `needs` generically, but the dependency list itself is
# manual. Adding a job to ci.yml without adding it to `ci-gate.needs` produces a job
# that can fail while the required status check stays green — a gate with a hole in
# it, and the hole is invisible because everything reports success.
#
# Usage: verify-ci-gate-covers-jobs.sh <workflow.yml> <aggregate-job-id>

set -euo pipefail

WORKFLOW="${1:-.github/workflows/ci.yml}"
GATE_JOB="${2:-ci-gate}"

[ -f "${WORKFLOW}" ] || {
  echo "❌ workflow not found: ${WORKFLOW}" >&2
  exit 1
}

python3 - "${WORKFLOW}" "${GATE_JOB}" <<'PY'
import re
import sys

path, gate = sys.argv[1], sys.argv[2]
text = open(path).read()

jobs_block = re.search(r"^jobs:\n(.*)\Z", text, re.S | re.M)
if not jobs_block:
    print(f"❌ no jobs: block in {path}")
    sys.exit(1)

job_ids = re.findall(r"^  ([A-Za-z0-9_-]+):\s*$", jobs_block.group(1), re.M)
if gate not in job_ids:
    print(f"❌ aggregate job '{gate}' not found in {path} (jobs: {', '.join(job_ids)})")
    sys.exit(1)

gate_block = re.search(
    rf"^  {re.escape(gate)}:\s*$\n((?:    .*\n|\n)*)", jobs_block.group(1), re.M
)
needs_decl = re.search(r"^    needs:\s*(.+)$", gate_block.group(1), re.M)
declared = set()
if needs_decl:
    declared = {n.strip(" []") for n in needs_decl.group(1).split(",") if n.strip(" []")}

expected = {j for j in job_ids if j != gate}
missing = sorted(expected - declared)
unknown = sorted(declared - expected)

print(f"jobs in {path}: {', '.join(sorted(job_ids))}")
print(f"{gate}.needs: {', '.join(sorted(declared)) or '(none)'}")

if unknown:
    print(f"❌ {gate}.needs references jobs that do not exist: {', '.join(unknown)}")
if missing:
    print(f"❌ these jobs are not covered by {gate}: {', '.join(missing)}")
    print("   They can fail while the required status check stays green.")
    print(f"   Add them to `needs:` of the {gate} job.")

sys.exit(1 if (missing or unknown) else 0)
PY

echo "✅ ${GATE_JOB} covers every job in ${WORKFLOW}"
