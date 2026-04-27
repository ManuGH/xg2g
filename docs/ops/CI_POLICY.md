# CI Policy

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
- Historical workflow `.github/workflows/ui-contract.yml` is not the source of
  truth for PR enforcement anymore. It remains as a push/manual backstop and
  branch-protection compatibility shim until an explicit cutover removes it.

## Repo Hygiene
- `.github/workflows/repo-health.yml` runs on PRs and on `push(main)`.
- It is the normative authority for repository-health merge invariants.

## Security and Deep Scans (Async)
- Security scans and deep checks must stay non-required unless explicitly listed in
  required branch protection:
  - workflow_dispatch
  - scheduled runs
  - push/tag backstops
  - optional pull_request backstops
- These scans must not block merges by default.
- `.github/workflows/ci-deep-scheduled.yml` has two scheduled suites under one workflow:
  - `0 2 * * *`: nightly race, integration, and spec/doc lint
  - `0 3 * * 0`: weekly security and governance
- Scheduled deep-suite run names, summaries, and alert issues must include the
  concrete suite label (`nightly` or `weekly`) so failures are triaged against
  the right cron.
- Pinned Go security tools must be built with the active repository toolchain
  from `go env GOVERSION`; scanner binaries built with an older Go release can
  fail against the backend module's `go` directive.

## Offline Rules
- No @latest in tools.
- No curl | sh.
- No implicit toolchain downloads.
- Remote GitHub Actions in `.github/workflows/*.yml` must be pinned to full
  40-character commit SHAs. Version tags such as `@v4` are not acceptable for
  repo workflows.
- Go deps are vendored; builds use -mod=vendor.
- make ci-pr must succeed with:
  - GOTOOLCHAIN=local
  - GOPROXY=off GOSUMDB=off GOVCS="*:off"

## WebUI
- Node 24 LTS is the pinned WebUI runtime via `.node-version`; GitHub Actions must use `node-version-file: .node-version`.
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
| API Docs (GitHub Pages) | `push(main)`, `workflow_dispatch` | Publishes static API docs when API docs inputs change |
| CI Deep Scheduled | `schedule`, `workflow_dispatch` | Single workflow with separate `nightly` and `weekly` run labels |
| Coverage | `pull_request`, `push(main)`, `workflow_dispatch` | Backend coverage backstop for Go-related diffs |
| Lint Invariants | `pull_request`, `push(main)`, `workflow_dispatch` | Scoped invariant backstop; fail-closed scope |
| UI Contract Enforcement | `pull_request`, `push(main)`, `workflow_dispatch` | PR compatibility shim; push/manual backstop |
| CodeQL | `pull_request`, `push(main)`, `schedule`, `workflow_dispatch` | Security scan; async unless required by branch protection |
| Gosec | `pull_request`, `push(main)`, `schedule`, `workflow_dispatch` | Security scan; async unless required by branch protection |
| Govulncheck | `pull_request`, `push(main)`, `schedule`, `workflow_dispatch` | Security scan; async unless required by branch protection |
| Scorecard | `workflow_dispatch` | Security (async) |
| Security - Trivy | `pull_request`, `push(main)`, `schedule`, `workflow_dispatch` | Security scan; enforcement opt-in |
| docker | `push(main)`, `workflow_dispatch` | Main-branch image publishing pipeline |
| Container Security | `push(tags)`, `workflow_dispatch` | Release pipeline |
| ffmpeg-base | `push(main)`, `workflow_dispatch` | Pinned FFmpeg base image publishing pipeline |
| Release | `push(tags)` | Release pipeline |

## Branch Protection Debt
- Stable historical workflow/job names are currently preserved to avoid accidental branch-protection drift.
- That is intentional technical debt, not a permanent design goal.
- Any future cleanup that removes or renames these checks must be done as an explicit branch-protection cutover, with this document updated in the same change.

## Authority
- Repository-health invariants are normatively defined by `.github/workflows/repo-health.yml`.
- Local Make targets may reproduce those checks for convenience, but they are wrappers, not a second source of merge policy truth.
