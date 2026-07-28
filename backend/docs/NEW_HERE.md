# New Here (10 Minutes)

This is the fastest path to understand and run xg2g as a new contributor.

## 1. Read Order

1. `README.md` - product scope and quickstart
2. `docs/README.md` - documentation portal
3. `docs/dev/REPO_MAP.md` - what to edit, what to run, what to ignore
4. `docs/dev/SETUP.md` - local setup and pinned tooling
5. `docs/arch/PACKAGE_LAYOUT.md` - where code belongs
6. `docs/ops/CI_FAILURE_PLAYBOOK.md` - required local/CI proof path

## 2. First Local Run

```bash
make install
make doctor
make start
```

Optional WebUI dev loop:

```bash
cd frontend/webui
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
cd backend && go test ./...
cd frontend/webui && npm run test
```

## 4. Where To Change What

- API and handlers: `backend/internal/control/http/v3/`
- Playback/session domain behavior: `backend/internal/control/` and `backend/internal/domain/`
- App wiring/bootstrap: `backend/internal/app/bootstrap/`
- Frontend: `frontend/webui/src/`
- Contracts and invariants: `docs/ops/`, `docs/arch/`, `docs/decision/`

## 5. If CI Fails

Start here: `docs/ops/CI_FAILURE_PLAYBOOK.md`

Common checks you can run locally:

- `./backend/scripts/check-go-toolchain.sh`
- `./backend/scripts/check-ui-contract.sh`
- `cd frontend/webui && npm run verify:client-wrapper`

## 6. Contributor Notes

- Do not push directly to `main`.
- Prefer targeted changes with matching tests.
- Keep generated artifacts in sync when changing templates/contracts.
