# xg2g WebUI

Web frontend for xg2g (`/ui/`) built with React + TypeScript + Vite.

## Prerequisites

- Node.js 22+
- npm
- Backend API running on `http://localhost:8080` for local dev proxy

## Quick Start

```bash
cd frontend/webui
npm ci
npm run dev
```

Open: `http://localhost:5173/ui/`

## Fast Local UI Iteration

For backend + UI development without rebuilding the production container image:

```bash
make backend-dev-ui   # backend on http://localhost:8080 with -tags=dev
make webui-dev        # Vite with HMR on http://localhost:5173
```

Then open `http://localhost:8080/ui/`.

One-command variant:

```bash
make dev-ui
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

- Design contract: `frontend/webui/DESIGN.md`
- UI contract gate script: `../../backend/scripts/check-ui-contract.sh`
- API contract source: `../../backend/api/openapi.yaml`

## Project Layout

- `src/` - production WebUI code
- `tests/` - integration/contract-focused UI tests
- `scripts/` - local verification helpers
- `dist/` - Vite build output (used for backend embed)

## Security Note

Auth tokens are stored in `sessionStorage` (not `localStorage`) to avoid
persistence across browser restarts. Clearing auth state removes stored token
and auth headers.
