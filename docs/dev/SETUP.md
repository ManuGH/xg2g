# Developer Setup

Run this once after cloning:

```bash
make dev-tools
make install
make check-tools
```

Notes:
- `make dev-tools` installs pinned tool versions, including `golangci-lint`.
- Pre-push hooks use the repo-managed `golangci-lint` and fail with `Run: make dev-tools` if missing or mismatched.
- Avoid `--no-verify` for normal development; rely on reproducible local + CI gates.
