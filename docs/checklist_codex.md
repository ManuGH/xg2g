# Codex Review Checklist

Use this short guide to run Codex/QA checks against xg2g without digging through the repo.

## Prerequisites

- Go 1.25.5 in `PATH` (see README for `go install golang.org/dl/go1.25.5@latest` helper)
- Docker 27+ for compose workflows
- Node.js 22+ and npm (only if you build the WebUI via `make ui-build`)
- `make`; optional: `python3 -m pip install check-jsonschema` for schema checks

## Setup

- Clone: `git clone https://github.com/ManuGH/xg2g && cd xg2g`
- Config: `cp .env.example .env` and adjust `OWI_HOST`, `DEFAULT_BOUQUET`, ports, and tokens as needed
- Optional: install tooling once with `make dev-tools` (golangci-lint, govulncheck, syft/grype, go-licenses)

## Automated Checks (one command)

- `make codex` → runs `golangci-lint`, race + coverage tests (`test-cover`), and `govulncheck`

## Additional / Optional Checks

- `make lint` / `make lint-fix` for linting only
- `make test` for unit tests with the stub transcoder; `make test-race` to include the race detector
- `make schema-validate` to validate example configs against the JSON schema (requires `check-jsonschema`)
- `make security` for the full security bundle (SBOM + grype + nancy + govulncheck)
- `make ui-build` to build WebUI assets if you change `webui/`

## Quick Validation

1. `docker compose up -d --build`
2. `curl http://localhost:8080/api/v2/status` -> should return JSON health
3. `curl http://localhost:8080/files/playlist.m3u` -> download playlist

## Notes for Reviewers

- No secrets are committed; `.env` and `config.yaml` are intentionally ignored. Use `.env.example` and `config.example.yaml` as templates.
- GPU transcoding (Mode 3) is experimental and disabled by default; tests do not require it.
- Integration tests for the transcoder path require the Rust library and proper `LD_LIBRARY_PATH`; stick to `make codex` unless you explicitly need those paths.
