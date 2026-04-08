# Developer Setup

## Prerequisites

| Tool | Version | Notes |
| :--- | :--- | :--- |
| **Go** | 1.25.8+ | Pinned in `backend/go.mod` |
| **Node.js** | 22+ | Used for WebUI build and tests |
| **Docker** | Recent | Required for container builds and integration tests |
| **Make** | GNU Make | Build orchestration |

Optional helpers:

- `mise`: run `mise install` from [mise.toml](../../mise.toml) to provision the
  pinned local Go and Node versions before you start.
- `.devcontainer/`: reopen the repo in
  [.devcontainer/devcontainer.json](../../.devcontainer/devcontainer.json) for a
  containerized workstation that bootstraps the repo with the same
  `make install`, `make dev-tools`, and `make doctor` flow.

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
make start         # standard local path (Compose)
make start-gpu     # Linux /dev/dri path for VAAPI, QuickSync, AMD
make start-nvidia  # NVIDIA overlay path for NVENC
make dev-ui        # frontend HMR path on http://localhost:8080/ui/
make backend-dev   # backend only, foreground, no containers
```

For a broader local gate, run:

```bash
make ci-pr
```

## Notes

- Pre-push hooks use the repo-managed `golangci-lint` and fail with
  `Run: make dev-tools` if missing or mismatched.
- `make stop` is the matching shutdown command for the default `make start`
  path.
- `make stop-gpu` and `make stop-nvidia` are the matching shutdown commands for
  the hardware-specific container paths.
- Avoid `--no-verify` for normal development; rely on reproducible local
  and CI gates.
- For the full development workflow (dev loop, UI hot-reload, Compose),
  see the [Development Guide](../guides/DEVELOPMENT.md).
