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
- 1 approving review required.
- Required CI checks must be green.

