# Repository Map

This is the quick orientation map for contributors who do not know the repo yet.
It is intentionally shorter than the architecture docs and focuses on what to
edit, what to run, and what to ignore.

## Start Here

Read these in order:

1. [README.md](../../README.md) - product scope and operator quickstart
2. [docs/dev/SETUP.md](SETUP.md) - local toolchain and first run
3. [docs/arch/PACKAGE_LAYOUT.md](../arch/PACKAGE_LAYOUT.md) - backend ownership rules
4. [docs/ops/CI_FAILURE_PLAYBOOK.md](../ops/CI_FAILURE_PLAYBOOK.md) - required proof path when CI is red

## Root Directory

| Path | Role |
| :--- | :--- |
| `.github/` | GitHub workflows, PR templates, security config, and workflow docs |
| `.devcontainer/` | Containerized workstation definition |
| `backend/` | Go backend, API contracts, generated server code, scripts, tests, vendored Go deps |
| `frontend/webui/` | React/Vite WebUI source, tests, design gates, generated TypeScript API client |
| `android/` | Android/TV companion app and smoke assets |
| `deploy/` | Production/local Compose files and deployment README |
| `docs/` | Architecture, operations, release, WebUI, and contributor documentation |
| `mk/` | Make target fragments used by the root `Makefile` |
| `monitoring/` | Observability config used by monitoring compose paths |
| `openapi/` | Published OpenAPI entrypoints |

## Source, Generated, Local

Some ignored local output directories may exist in the repository root after
development or test runs. Treat them as runtime state, not as source areas.

| Class | Paths | Rule |
| :--- | :--- | :--- |
| Primary source | `backend/internal/`, `backend/cmd/`, `frontend/webui/src/`, `android/` | Edit directly with matching tests |
| Contracts | `backend/api/openapi.yaml`, `backend/contracts/`, `docs/decision/`, `docs/ops/*CONTRACT*` | Treat as normative; update generated artifacts when changed |
| Generated but committed | `backend/internal/control/http/v3/server_gen.go`, `frontend/webui/src/client-ts/`, `backend/internal/control/http/dist/` | Regenerate through Make/npm targets; do not hand-edit unless the generator source is broken |
| Vendored dependencies | `backend/vendor/` | Updated through Go module/vendor workflow only |
| Committed test assets | `backend/testdata/`, `backend/fixtures/`, selected Android assets | Keep intentional and small enough for repo-health gates |
| Local-only outputs | `data/`, `logs/`, `artifacts/`, `test-results/`, `node_modules/`, `.venv/`, `bin/`, `frontend/webui/dist/` | Do not commit; clean only when you know the running service does not need them |

## Common Change Paths

| Task | Start Here | Typical Verification |
| :--- | :--- | :--- |
| Backend API behavior | `backend/internal/control/http/v3/`, then domain/control package from `PACKAGE_LAYOUT.md` | `cd backend && go test ./...` or `make ci-pr` |
| Playback decision behavior | `backend/internal/control/playback/`, `backend/internal/domain/`, decision docs | Backend tests plus relevant WebUI contract tests |
| WebUI feature work | `frontend/webui/src/features/` | `npm --prefix frontend/webui test` and `npm --prefix frontend/webui run lint` |
| Embedded WebUI dist | `frontend/webui/` source first | `make ui-build` and `make verify-embedded-webui-dist` |
| Config surface | `backend/internal/config/registry.go` | `make generate-config` and `make verify-config` |
| OpenAPI/client contract | `backend/api/openapi.yaml` | `make gen-openapi-hard` and `make verify-openapi-hard-mode` |
| CI/workflow behavior | `.github/workflows/`, `.github/WORKFLOWS.md`, `docs/ops/CI_POLICY.md` | `actionlint` and `./backend/scripts/verify-action-pins.sh --local-only` |

## Gate Map

Use the smallest gate that proves the change, then run the broader gate before a PR.

| Gate | Purpose |
| :--- | :--- |
| `make doctor` | Local workspace sanity: pinned Go/Node, required tools, `.env`, WebUI deps |
| `make ci-pr` | Main local PR proof bundle |
| `make verify` | Broad governance verification |
| `make quality-gates-offline` | Offline-safe contract and generated-artifact checks |
| `npm --prefix frontend/webui run lint` | WebUI lint, logging gate, design gates |
| `npm --prefix frontend/webui test` | WebUI unit/contract tests |
| `make verify-embedded-webui-dist` | Proves committed backend WebUI assets match the frontend build |
| `make workspace-clean-preview` | Shows which ignored local build/test outputs can be safely cleaned |

## Before You Commit

Run:

```bash
git status --short
git diff --check
```

If generated files changed, verify the matching generator target. If local-only
runtime directories appear in `git status`, fix `.gitignore` or the hygiene gate
instead of committing the output.
