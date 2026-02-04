# Workflow (Mac + Linux)

This repo is developed on macOS (edit + test only) and Linux (test + run). The goal is a clean `main` with PR-only merges.

## Branching

- Never push directly to `main`.
- Always work on a feature branch: `codex/<feature>` or `<user>/<feature>`.

```bash
git checkout -b codex/<feature>
```

## Keeping Branches Up to Date (Rebase Standard)

```bash
git fetch origin
git checkout <feature>
git rebase origin/main
git push --force-with-lease
```

## Testing

### macOS (edit + test)

```bash
go test ./...
```

### Linux (test + run)

```bash
go test ./...
# build/run as needed
make build
./xg2g --config /path/to/config.yaml
```

## PR Rules

- `main` requires PRs.
- Review required in team setups; for solo work reviews can be optional.
- Required CI checks must be green.

## Deployment (Homelab + Real Ops)

Canonical mode is **systemd supervising Docker Compose** on Linux (homelab and production).
This matches `docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md` and avoids drift.

- Use `/srv/xg2g` as working directory.
- Use `/srv/xg2g/docker-compose.yml`.
- Use `/var/lib/xg2g` for data.
- Manage via `systemctl` (start/reload/stop).

Direct `docker compose up` is acceptable only for short-lived dev/testing.
