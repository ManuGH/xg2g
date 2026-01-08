# CI Contract: Local Verification Suite

This document defines the mapping between CI jobs and their local CLI settings.
All developers must verify these gates locally before pushing.

## 1. Quality Gates (Pre-Commit)

| CI Job | Local Command | Description |
| :--- | :--- | :--- |
| `lint` | `make lint` | Runs `golangci-lint`. |
| `check-deprecations` | `python3 scripts/check_deprecations.py` | Registry validation. |
| `complexity-check` | `gocyclo -over 20 ./internal ./cmd` | Complexity limit. |
| `openapi-drift` | `make generate && git diff --exit-code` | Spec sync check. |

## 2. Testing & Validation

| CI Job | Local Command | Description |
| :--- | :--- | :--- |
| `Unit tests` | `go test ./...` | Unit tests (no GPU). |
| `Integration` | `make smoke-test` | Local daemon run. |
| `validate-config` | `make validate` | Config validation. |
| `schema-validate` | `make schema-validate` | JSON Schema check. |

## 3. Build Sanity

| CI Job | Local Command | Description |
| :--- | :--- | :--- |
| `nogpu-build` | `go build -tags=nogpu ./cmd/daemon` | Base compilation. |
| `multi-platform` | `make build-all` | Cross-compilation. |

## 4. Environment Invariants

- **Config Determinism**: `internal/config` is the ONLY package allowed to read os.
- **Reproducible Builds**: Built with `SOURCE_DATE_EPOCH` from git history.
