# New Here (10 Minutes)

This is the fastest path to understand and run xg2g as a new contributor.

## 1. Read Order (3 files)

1. `README.md` - Product scope and quickstart
2. `WORKFLOW.md` - Branching and PR rules
3. `docs/arch/PACKAGE_LAYOUT.md` - Where code belongs

## 2. First Local Run

```bash
make hooks
make check-env
make build
```

Optional WebUI dev loop:

```bash
cd webui
npm ci
npm run dev
```

## 3. First Quality Check

```bash
make ci-pr
```

If that is too heavy locally, use this narrower path first:

```bash
make lint
go test ./...
cd webui && npm run test
```

## 4. Where To Change What

- API and handlers: `internal/control/http/v3/`
- Playback/session domain behavior: `internal/control/` and `internal/domain/`
- App wiring/bootstrap: `internal/app/bootstrap/`
- Frontend: `webui/src/`
- Contracts and invariants: `docs/ops/`, `docs/arch/`, `contracts/`

## 5. If CI Fails

Start here: `docs/ops/CI_FAILURE_PLAYBOOK.md`

Common checks you can run locally:

- `./scripts/check-go-toolchain.sh`
- `./scripts/check-ui-contract.sh`
- `cd webui && npm run verify:client-wrapper`

## 6. Contributor Notes

- Do not push directly to `main`.
- Prefer targeted changes with matching tests.
- Keep generated artifacts in sync when changing templates/contracts.
