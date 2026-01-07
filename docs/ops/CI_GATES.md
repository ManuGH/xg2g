# CI Quality Gates

> **Status**: enforced by `.github/workflows/ci-v2.yml` and `phase4-guardrails.yml`.

## 1. Merge Gate (Required for all PRs)

Every Pull Request must pass the following checks before merging to `main`:

| Category | Job Name | Check Description | Failure Policy |
|----------|----------|-------------------|----------------|
| **Build** | `unit-tests` | `go build ./...` (Daemon + Validate tool) | **Blocker** |
| **Build** | `unit-tests` | `npm run build` (WebUI) | **Blocker** |
| **Logic** | `unit-tests` | `go test -race ./...` (Unit Tests) | **Blocker** |
| **Logic** | `integration-tests` | `make smoke-test` (Startup & Config) | **Blocker** |
| **Contract** | `unit-tests` | `git diff --exit-code` (Codegen Drift) | **Blocker** |
| **Quality** | `lint` | `golangci-lint` (Go Strict Linting) | **Blocker** |
| **Quality** | `lint` | `redocly lint` (OpenAPI Spec) | **Blocker** |
| **Quality** | `schema-validation` | `check-jsonschema` (Config Examples) | **Blocker** |
| **Guardrails** | `phase4-guardrails` | No new `.jsx` files (TypeScript Only) | **Blocker** |
| **Guardrails** | `phase4-guardrails` | No legacy `src/client` imports | **Blocker** |

## 2. Release Gate (Required for Version Tags)

In addition to the Merge Gates, a Release Candidate must pass:

| Category | Job Name | Check Description | Failure Policy |
|----------|----------|-------------------|----------------|
| **supply-chain** | `reproducible-build` | Binary SHA256 matches ref build | **Blocker** |
| **native** | `transcoder-tests` | Rust Transcoder FFI Tests | **Blocker** |

## 3. Local-Only Checks (Recommended before Push)

Developers should run these locally to avoid CI churn:

```bash
# Full Pre-Push Suite
make clean
make generate
make verify    # Runs lint, test, and vet
make smoke-test
```
