# CI Policy (CTO Mandate)

Purpose: Make CI deterministic, offline-reproducible, and not dependent on GitHub runner availability.

## Truth Sources
- Core build/test semantics should stay reproducible through Make targets.
- GitHub workflow/job names are the source of truth for merge enforcement because GitHub branch protection evaluates those checks directly.
- Required PR protection is intentionally split across a small set of workflows:
  - `.github/workflows/ci.yml` for the broad make-based PR gate
  - `.github/workflows/pr-required-gates.yml` for the diff-scoped WebUI build + browser-smoke gate
  - `.github/workflows/repo-health.yml` for repository hygiene checks
  - `.github/workflows/lint.yml` for scoped invariant jobs with stable historical job names
- Offline reproducibility is mandatory for the core gate.
- Live GitHub enforcement must still be verified separately.
  See `BRANCH_PROTECTION_CUTOVER.md` for the current GitHub-side inventory.

## Scope Policy
- Any diff-scoped workflow decision is security-relevant and must be fail-closed.
- If scope resolution is ambiguous, missing commit context, or diff computation fails, the workflow must run the broader check set instead of skipping.
- `.github/workflows/lint.yml` uses `backend/scripts/ci/resolve-lint-scope.sh` for this reason.

## PR Gate (Required Core)
- Primary workflow: `.github/workflows/ci.yml`
- Primary job: `make ci-pr`
- Constraints:
  - Max runtime: 15 minutes
  - One runner
  - No network dependency beyond checkout
  - No tool downloads in the gate
  - No flake tolerance

## Diff-Scoped PR Gates
- `.github/workflows/pr-required-gates.yml` owns only the canonical PR-time WebUI integration gate:
  - WebUI production build
  - browser-backed WebUI smoke
- Historical workflows `.github/workflows/ui-contract.yml` and `.github/workflows/phase4-guardrails.yml`
  are not the source of truth for PR enforcement anymore. They remain as push/manual
  backstops and branch-protection compatibility shims until an explicit cutover removes them.

## Repo Hygiene
- `.github/workflows/repo-health.yml` runs on PRs and on `push(main)`.
- It is the normative authority for repository-health merge invariants.

## Security and Deep Scans (Async)
- Security scans and deep checks must be async:
  - workflow_dispatch
  - push tags
  - optional nightly (only if runner capacity allows)
- These scans must not block merges.

## Offline Rules
- No @latest in tools.
- No curl | sh.
- No implicit toolchain downloads.
- Go deps are vendored; builds use -mod=vendor.
- make ci-pr must succeed with:
  - GOTOOLCHAIN=local
  - GOPROXY=off GOSUMDB=off GOVCS="*:off"

## WebUI
- Node is optional for the primary broad PR gate in `.github/workflows/ci.yml`.
- Node is required only when the dedicated WebUI integration gate in `.github/workflows/pr-required-gates.yml` is in scope.

## Flake Policy
- Flaky tests are treated as bugs.
- Any flake is removed or fixed before it can be required.

## Operational Documents

- **CI Failure Playbook**  
  See `CI_FAILURE_PLAYBOOK.md` for the mandatory triage and fix path when required PR checks fail.

- **External Audit Mode**  
  See `EXTERNAL_AUDIT_MODE.md` for source-only ZIP review and offline verification protocol.

## Active Workflow Triggers

| Workflow | Triggers | Notes |
| --- | --- | --- |
| CI (PR Gate) | `pull_request`, `push(main)`, `workflow_dispatch` | Required check |
| PR Required Gates | `pull_request`, `workflow_dispatch` | Canonical WebUI build + browser smoke gate |
| Repository Health Checks | `pull_request`, `push(main)` | Required checks |
| Runner Smoke Test | `workflow_dispatch` | Diagnostic only |
| CI Nightly | `workflow_dispatch` | Deep/expensive |
| Lint Invariants | `pull_request`, `push(main)`, `workflow_dispatch` | Scoped invariant backstop; fail-closed scope |
| Phase 4 Guardrails | `pull_request`, `push(main)`, `workflow_dispatch` | PR compatibility shim; push/manual backstop |
| UI Contract Enforcement | `pull_request`, `push(main)`, `workflow_dispatch` | PR compatibility shim; push/manual backstop |
| CodeQL | `workflow_dispatch` | Security (async) |
| Gosec | `workflow_dispatch` | Security (async) |
| Govulncheck | `workflow_dispatch` | Security (async) |
| Scorecard | `workflow_dispatch` | Security (async) |
| Docker | `push(tags)`, `workflow_dispatch` | Release pipeline |
| Container Security | `push(tags)`, `workflow_dispatch` | Release pipeline |
| Release | `push(tags)`, `workflow_dispatch` | Release pipeline |

## Branch Protection Debt
- Stable historical workflow/job names are currently preserved to avoid accidental branch-protection drift.
- That is intentional technical debt, not a permanent design goal.
- Any future cleanup that removes or renames these checks must be done as an explicit branch-protection cutover, with this document updated in the same change.

## Authority
- Repository-health invariants are normatively defined by `.github/workflows/repo-health.yml`.
- Local Make targets may reproduce those checks for convenience, but they are wrappers, not a second source of merge policy truth.
