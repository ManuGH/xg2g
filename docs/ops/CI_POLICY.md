# CI Policy (CTO Mandate)

Purpose: Make CI deterministic, offline-reproducible, and not dependent on GitHub runner availability.

## Truth Sources
- SSOT: Make targets are the source of truth, not workflow YAML.
- Required gate: exactly one PR gate (ci.yml).
- Offline reproducibility is mandatory for the core gate.

## PR Gate (Required)
- Single workflow: .github/workflows/ci.yml
- Single job: make ci-pr
- Constraints:
  - Max runtime: 15 minutes
  - One runner
  - No network dependency beyond checkout
  - No tool downloads in the gate
  - No flake tolerance

## Repo Hygiene (PR Only)
- .github/workflows/repo-health.yml runs on PRs only.

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
- Node is optional and never required for PR gate.
- WebUI build is opt-in (build-with-ui) and only when needed.

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
| Repository Health Checks | `pull_request`, `push(main)` | Required checks |
| Runner Smoke Test | `workflow_dispatch` | Diagnostic only |
| CI Nightly | `workflow_dispatch` | Deep/expensive |
| Lint Invariants | `workflow_dispatch` | Informational |
| Phase 4 Guardrails | `workflow_dispatch` | Informational |
| UI Contract Enforcement | `workflow_dispatch` | Informational |
| CodeQL | `workflow_dispatch` | Security (async) |
| Gosec | `workflow_dispatch` | Security (async) |
| Govulncheck | `workflow_dispatch` | Security (async) |
| Scorecard | `workflow_dispatch` | Security (async) |
| Docker | `push(tags)`, `workflow_dispatch` | Release pipeline |
| Container Security | `push(tags)`, `workflow_dispatch` | Release pipeline |
| Release | `push(tags)`, `workflow_dispatch` | Release pipeline |
