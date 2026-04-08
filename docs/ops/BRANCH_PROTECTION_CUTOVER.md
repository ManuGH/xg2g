# Branch Protection Cutover

Purpose: capture the real GitHub `main` protection state before changing required checks or deleting compatibility workflows.

## Snapshot

- Repository: `ManuGH/xg2g`
- Branch: `main`
- Snapshot date: `2026-04-08`
- Source of truth for this snapshot: live GitHub settings via `gh api`, not checked-in workflow YAML

## Inventory Commands

```bash
gh api repos/ManuGH/xg2g/branches/main/protection
gh api graphql -f query='query { repository(owner:"ManuGH", name:"xg2g") { branchProtectionRules(first: 20) { nodes { pattern requiresApprovingReviews requiresCodeOwnerReviews requiresConversationResolution isAdminEnforced requiredStatusCheckContexts requiredStatusChecks { context app { slug } } } } } }'
gh api repos/ManuGH/xg2g/rulesets
gh api repos/ManuGH/xg2g/rulesets/8619655
gh pr view 353 --json mergeStateStatus,mergeable,statusCheckRollup
```

## Observed GitHub State

### `main` branch protection

Observed on `2026-04-08`:

- `enforce_admins`: `true`
- `required_linear_history`: `true`
- `required_conversation_resolution`: `false`
- `allow_force_pushes`: `false`
- `allow_deletions`: `false`
- `required_status_checks`: not enabled

The REST endpoint `repos/ManuGH/xg2g/branches/main/protection/required_status_checks` returned `404 Required status checks not enabled`.

### Branch protection rule (GraphQL)

Observed on `2026-04-08`:

- branch protection rule pattern: `main`
- `requiredStatusCheckContexts`: `[]`
- `requiredStatusChecks`: `[]`
- `isAdminEnforced`: `true`

### Active repository ruleset

Observed on `2026-04-08`:

- active branch ruleset: `main-protection`
- scope: `refs/heads/main`
- active rule types:
  - `non_fast_forward`
- no required-workflow or required-status-check rule present in the repository ruleset snapshot

## Merge Semantics Check

Observed on open PR `#353` on `2026-04-08`:

- failing check present: `CI / PR Gate (Fast & Deterministic)`
- PR state:
  - `mergeStateStatus`: `UNSTABLE`
  - `mergeable`: `MERGEABLE`

Interpretation:

- checks are present in PR rollup
- but GitHub is not currently enforcing them as merge-blocking required checks on `main`

This means the next system step is not a pure required-checks cutover. The first real GitHub-side change will be establishing an explicit canonical required set.

## Current Check Taxonomy

### Candidate canonical PR checks

These are the checks that currently carry the intended protection semantics and should be the first candidates for any future required set:

- `CI / PR Gate (Fast & Deterministic)` from `.github/workflows/ci.yml`
- `Required Gates` from `.github/workflows/pr-required-gates.yml`
- `Prevent Large Files` from `.github/workflows/repo-health.yml`
- `Verify Test Assets in testdata/` from `.github/workflows/repo-health.yml`
- `Repo Hygiene` from `.github/workflows/repo-health.yml`
- `Docs Drift` from `.github/workflows/repo-health.yml`
- `check-env-access` from `.github/workflows/lint.yml`
- `check-deprecations` from `.github/workflows/lint.yml`
- `webui-lint` from `.github/workflows/lint.yml`

### Compatibility-only workflow names

These workflows are no longer the source of PR enforcement and are planned deletion candidates after a successful branch-protection cutover:

- `.github/workflows/ui-contract.yml`
- `.github/workflows/phase4-guardrails.yml`

They currently remain only as:

- push/manual backstops
- historical name placeholders while branch protection still has not been explicitly re-established on the canonical set

## Documentation Drift Found

The live GitHub state on `2026-04-08` does not match the repo’s intended-policy language in these files:

- `docs/ops/CI_FAILURE_PLAYBOOK.md`
- `docs/ops/SOLO_MAINTAINER_MERGE_POLICY.md`
- `docs/ops/CI_POLICY.md`

Those documents describe the target safety model. They must not be read as proof that GitHub is currently enforcing required checks.

## Planned Cutover Phases

### Phase 1: inventory

- completed in-repo by this snapshot
- result: there is no active GitHub required-check set to “switch”; the next step is to define and establish one

## Proposed Initial Required Set

Goal: establish the smallest real merge gate that is broad, deterministic, and operationally sustainable.

### Required on `main` in the first activation

1. `CI / PR Gate (Fast & Deterministic)`
   - why required:
     - primary broad merge gate
     - already framed as the canonical PR gate in repo policy
     - covers build/test/governance checks that are not frontend-diff-specific
   - source:
     - `.github/workflows/ci.yml`

2. `Required Gates`
   - why required:
     - canonical PR-time WebUI integration gate
     - single aggregated PR check, safer to require than multiple scoped detail jobs
     - carries the WebUI production build and browser-smoke semantics intended for modern WebUI changes
   - source:
     - `.github/workflows/pr-required-gates.yml`

3. `Prevent Large Files`
   - why required:
     - repository invariant not covered by the primary build/test gate
     - deterministic and cheap
     - protects clone/CI health and artifact-drift hygiene
   - source:
     - `.github/workflows/repo-health.yml`

4. `Verify Test Assets in testdata/`
   - why required:
     - repository invariant independent from compile/test success
     - deterministic and cheap
     - prevents test/media sprawl in repo root
   - source:
     - `.github/workflows/repo-health.yml`

### Deliberately not required in the first activation

- `Repo Hygiene`
  - reason:
    - valuable, but broader than the minimal first enforcement set
    - overlaps partially with `gate-repo-hygiene` inside `CI / PR Gate`, but is not yet fully consolidated there
    - treat as a promotion candidate after live enforcement has stabilized

- `Docs Drift`
  - reason:
    - already materially covered by `verify-generated-artifacts -> verify-docs-compiled` inside `CI / PR Gate`
    - keeping it non-required avoids a redundant second merge blocker

- `check-env-access`
- `check-deprecations`
- `webui-lint`
  - reason:
    - these remain valuable invariant signals
    - but they are scoped/backstop jobs, not the preferred initial technical merge-enforcement basis
    - if they must become required later, prefer first folding the semantics into a canonical gate rather than promoting many detail-job names

- `UI Contract Enforcement`
- `Phase 4 Guardrails`
  - reason:
    - historical compatibility workflows only
    - not the source of PR enforcement anymore
    - planned deletion candidates after a successful cutover and observation window

- `CodeQL`, `Security - Gosec`, `govulncheck`, `Security - Trivy`, `Coverage`, `CI Nightly`, `CI Deep Scheduled`
  - reason:
    - async, advisory, slower, broader, or intentionally non-blocking
    - not appropriate for the smallest first required set

## Why this set is minimal

- one broad canonical code gate
- one canonical frontend/browser gate
- two cheap repository invariants that are not otherwise guaranteed by the broad gate

It avoids:

- requiring historical workflow names
- requiring many scope-sensitive detail jobs
- turning async security scans into merge-blocking noise
- overcompensating from “nothing enforced” to “everything enforced”

### Phase 2: parallel validation

Before changing GitHub settings:

- verify that the proposed required set above is sufficient on real PRs
- confirm compatibility workflows are only backstops, not carrying unique protection semantics
- observe multiple PR runs, including:
  - backend-only changes
  - frontend changes
  - doc-only changes

Recommended evidence before activation:

- at least 3 clean PR runs where all four proposed required checks appear as expected
- at least 1 frontend PR where `Required Gates` exercises the browser-smoke path
- at least 1 intentionally red test PR proving a failing candidate required check is the one you expect to block

## Activation Plan

### Step 1: enable the minimal required set

Planned operator command:

```bash
gh api \
  --method PATCH \
  -H "Accept: application/vnd.github+json" \
  repos/ManuGH/xg2g/branches/main/protection/required_status_checks \
  -f strict=true \
  -f contexts[]='CI / PR Gate (Fast & Deterministic)' \
  -f contexts[]='Required Gates' \
  -f contexts[]='Prevent Large Files' \
  -f contexts[]='Verify Test Assets in testdata/'
```

Inference from current GitHub docs: GitHub supports updating required status checks directly on the protected branch via the `required_status_checks` REST subresource.

### Step 2: negative test the gate

After activation:

1. open a test PR to `main`
2. make `CI / PR Gate (Fast & Deterministic)` fail intentionally
3. verify the PR is no longer mergeable under branch protection
4. repeat once for `Required Gates`

### Step 3: observation window

Keep historical compatibility workflows in place during the first live observation window.

Suggested window:

- 1 week, or
- 5 real PRs, whichever is later

Only after that should you:

- remove compatibility workflow names from policy/docs where still referenced
- delete `.github/workflows/ui-contract.yml`
- delete `.github/workflows/phase4-guardrails.yml`

### Phase 3: GitHub settings cutover

Planned operator action:

1. set `main` required checks to the proposed minimal canonical set
2. confirm merges are blocked only by the intended checks
3. delete compatibility workflows after the new set has run cleanly for a safe observation window

## Guardrail

Do not delete compatibility workflows before GitHub required checks have been explicitly updated and observed in live PRs.
