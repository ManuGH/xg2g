# Branch Protection Policy

This repository enforces a strict workflow on the `main` branch. Everyone – including maintainers – must follow the same rules to keep history auditable and builds trustworthy.

## `main` branch rules

- `main` is a protected branch; direct pushes, force pushes, or history rewrites are not allowed.
- Every change must arrive via a pull request. The PR must stay open until all required checks pass.
- At least one maintainer review is required. Any new commit invalidates previous reviews and needs fresh approval.
- Linear history is enforced. Use squash-merge or rebase-merge; merge commits are rejected.
- Required status checks (all must be green):
  - `build` (Go build)
  - `test` (unit tests)
  - `test-race` (race detector)
  - `lint` (golangci-lint)
  - `codeql` (GitHub CodeQL scanning)
  - `govulncheck` (Go vulnerability scanning)
- Security tooling (CodeQL, dependabot alerts, secret scanning) must remain enabled. Do not bypass failing security checks.

## Pull request expectations

- Keep PRs focused and well described; link to issues when relevant.
- Rebase your branch onto the latest `main` before requesting final review.
- Do not merge your own PR without review (unless a maintainer explicitly applies a temporary bypass for an emergency fix).
- Ensure all secrets remain out of commits; use repository secrets or Kubernetes secrets for runtime configuration.

Questions about this policy? Open a discussion or ping the maintainers via the contact information in `SECURITY.md`.
