# Developer Setup

## Prerequisites

| Tool | Version | Notes |
| :--- | :--- | :--- |
| **Go** | 1.25.9+ | Pinned in `backend/go.mod` |
| **Node.js** | 24 LTS | Pinned in `.node-version`, `.nvmrc`, and `mise.toml` |
| **Docker** | Recent | Required for container builds and integration tests |
| **Make** | GNU Make | Build orchestration |

Optional helpers:

- `mise`: run `mise install` from [mise.toml](../../mise.toml) to provision the
  pinned local Go and Node versions before you start.
- `.devcontainer/`: reopen the repo in
  [.devcontainer/devcontainer.json](../../.devcontainer/devcontainer.json) for a
  containerized workstation that bootstraps the repo with the same
  `make install`, `make dev-tools`, and `make doctor` flow.

If you are new to the repository, read the [Repository Map](REPO_MAP.md) after
setup. It explains the main directories, generated artifacts, and required
local gates.

## First-Time Setup

Run this once after cloning:

```bash
make install
make dev-tools
make start
```

What these targets do:

- `make install` creates `.env` from `.env.example` when needed, generates a local `XG2G_DECISION_SECRET`, and installs WebUI dependencies.
- `make dev-tools` installs the pinned local CLI toolchain used day to day (`golangci-lint`, `govulncheck`, Python helper venv).
- `make start` runs `make doctor`, validates the local Docker runtime, and then starts the default local Compose stack on `http://localhost:8088`.

Use `make doctor` on its own when you want to validate the local workspace without starting containers.

## Verify Your Setup

```bash
make build           # backend binary
make lint            # static analysis
cd backend && go test ./...           # backend tests
cd frontend/webui && npm run test     # frontend tests
```

Recommended start paths after setup:

```bash
make start                   # standard local Compose path (CPU)
make start RUNTIME=vaapi     # Linux /dev/dri path
make start RUNTIME=nvidia    # NVIDIA / NVENC path
make dev-ui                  # frontend HMR on http://localhost:8080/ui/
make backend-dev             # backend only, foreground, no containers
```

For a broader local gate, run:

```bash
make ci-pr
```

## Notes

- Pre-push hooks use the repo-managed `golangci-lint` and fail with
  `Run: make dev-tools` if missing or mismatched.
- `make stop RUNTIME=<same-value>` is the matching shutdown command for
  `make start RUNTIME=<value>`.
- The legacy `start-gpu`, `start-nvidia`, `stop-gpu`, and `stop-nvidia` aliases
  remain fail-safe compatibility shims, but are not part of the public workflow.
- Avoid `--no-verify` for normal development; rely on reproducible local
  and CI gates.
- For the full development workflow (dev loop, UI hot-reload, Compose),
  see the [Development Guide](../guides/DEVELOPMENT.md).
