# Workspace Cleanup Runbook

Use this when root disk usage exceeds 90% or before creating release-split branches.

## Quick Check

```bash
df -h /root /tmp
```

## Preview Cleanup (Safe)

```bash
scripts/ops/cleanup-workspace.sh
```

## Apply Cleanup

```bash
scripts/ops/cleanup-workspace.sh --apply
```

## Aggressive Cleanup (`/tmp/xg2g-*` stale dirs)

```bash
scripts/ops/cleanup-workspace.sh --apply --aggressive
```

## Notes

- Script removes only clean auxiliary worktrees (`.worktrees/*`, `/tmp/xg2g-*`).
- Dirty worktrees are skipped and must be handled manually.
- Root log artifacts (`build.log`, `full_v3_test*.log`, etc.) are removed.
- Hygiene gate blocks committed `.worktrees/` paths and transient log artifacts.

