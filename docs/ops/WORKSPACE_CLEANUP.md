# Workspace Cleanup Runbook

Use this when root disk usage exceeds 90% or before creating release-split branches.

## Quick Check

```bash
df -h /root /tmp
```

## Preview Cleanup (Safe)

```bash
make workspace-clean-preview
```

## Apply Cleanup

```bash
make workspace-clean
```

## Aggressive Cleanup (`/tmp/xg2g-*` stale dirs)

```bash
make workspace-clean-aggressive
```

## Notes

- Script removes only clean auxiliary worktrees (`.worktrees/*`, `/tmp/xg2g-*`).
- Dirty worktrees are skipped and must be handled manually.
- Reproducible local build/test outputs (`artifacts/`, `test-results/`,
  `playwright-report/`, coverage output, `frontend/webui/dist/`) are removed.
- `make workspace-clean-aggressive` also removes local `bin/` outputs.
- Runtime and dependency state (`data/`, `logs/`, `node_modules/`, `.venv/`) is
  reported but intentionally kept.
- Hygiene gate blocks committed `.worktrees/` paths and transient log artifacts.
