# Developer Setup

## Prerequisites

| Tool | Version | Notes |
| :--- | :--- | :--- |
| **Go** | 1.25.8+ | Pinned in `backend/go.mod` |
| **Node.js** | 22+ | Used for WebUI build and tests |
| **Docker** | Recent | Required for container builds and integration tests |
| **Make** | GNU Make | Build orchestration |

## First-Time Setup

Run this once after cloning:

```bash
make dev-tools
make install
make check-tools
```

`make dev-tools` installs pinned tool versions, including `golangci-lint`.

## Verify Your Setup

```bash
make build           # backend binary
make lint            # static analysis
cd backend && go test ./...   # backend tests
cd frontend/webui && npm ci && npm run test  # frontend tests
```

Or run the full PR gate locally:

```bash
make ci-pr
```

## Notes

- Pre-push hooks use the repo-managed `golangci-lint` and fail with
  `Run: make dev-tools` if missing or mismatched.
- Avoid `--no-verify` for normal development; rely on reproducible local
  and CI gates.
- For the full development workflow (dev loop, UI hot-reload, Compose),
  see the [Development Guide](../guides/DEVELOPMENT.md).
