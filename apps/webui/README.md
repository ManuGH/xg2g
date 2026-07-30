# xg2g WebUI

Web frontend for xg2g (`/ui/`) built with React + TypeScript + Vite.

For documentation beyond local commands, start with
[docs/webui/README.md](../../docs/webui/README.md).

## Prerequisites

- Node.js 24 LTS (pinned by the repo root `.node-version` / `.nvmrc`)
- npm
- Backend API running on `http://localhost:8080` for local dev proxy

## Quick Start

```bash
make install
make dev-ui
```

Open: `http://localhost:8080/ui/`

## Fast Local UI Iteration

For backend + UI development without rebuilding the production container image:

`make install` ensures `.env` exists, generates a local playback decision
secret, and installs `apps/webui` dependencies.

`make doctor` is available when you want a standalone workspace check before
starting the dev path.

If you want the advanced two-terminal variant:

```bash
make backend-dev-ui
make webui-dev
```

If you want raw Vite only:

```bash
cd apps/webui
npm run dev
```

Useful overrides:

- `XG2G_UI_DEV_PROXY_URL` overrides the Vite target for the dev-tagged backend
- `XG2G_UI_DEV_DIR` serves a local built `dist/` directory instead of proxying Vite

## Core Scripts

- `npm run dev` - start local Vite dev server
- `npm run build` - type-check + production build
- `npm run test` - run Vitest suite
- `npm run lint` - ESLint + design + wrapper-boundary gates
- `npm run verify:client-wrapper` - prevent direct generated-client imports outside `src/client-ts/`
- `npm run generate-client` - regenerate typed API client from `../../backend/api/openapi.yaml`

## Design And Contracts

- WebUI docs index: `../../docs/webui/README.md`
- Design contract: `apps/webui/DESIGN.md`
- UI contract gate script: `../../backend/scripts/check-ui-contract.sh`
- API contract source: `../../backend/api/openapi.yaml`

## Project Layout

- `src/` - production WebUI code
- `tests/` - integration/contract-focused UI tests
- `scripts/` - local verification helpers
- `dist/` - Vite build output (used for backend embed)

## Security Note

Auth tokens are stored in `sessionStorage` under `XG2G_API_TOKEN`, limiting a
bearer token to the current browser tab instead of keeping it across browser
restarts. Tokens written to `localStorage` by older builds are migrated once to
`sessionStorage` and then removed from persistent storage. A boot-token can be
injected via the URL hash (`#xg2g_boot_token=...`); it is consumed once, stored
for the current tab, and the hash is stripped via `history.replaceState`.
Clearing auth state removes the token from both storage locations and clears
auth headers.
