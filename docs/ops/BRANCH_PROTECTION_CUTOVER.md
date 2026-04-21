# Branch Protection Cutover

This note tracks the intended branch-protection baseline for `main` and the
operational cutover path from ad hoc checks to deterministic required gates.

## Intended Required Checks

- `CI / PR Gate (Fast & Deterministic)`
- `Prevent Large Files`
- `Verify Test Assets in testdata/`

## Current Operating Rule

Until GitHub branch protection matches the intended baseline, maintainers must
still treat the checks above as mandatory merge gates and prove them locally
when GitHub-side enforcement is absent or drifting.

## Cutover Steps

1. Confirm the canonical required-check list in the repository docs and CI
   policy.
2. Apply the same list in GitHub branch protection for `main`.
3. Verify that no informational workflows were promoted accidentally.
4. Re-check merge behavior after the next workflow rename or consolidation.

## Evidence

- Reference doc: [CI Policy](CI_POLICY.md)
- Failure handling: [CI Failure Playbook](CI_FAILURE_PLAYBOOK.md)
- Solo-maintainer behavior: [SOLO_MAINTAINER_MERGE_POLICY.md](SOLO_MAINTAINER_MERGE_POLICY.md)
