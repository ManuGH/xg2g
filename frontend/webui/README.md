# xg2g WebUI

Web frontend for xg2g (`/ui/`) built with React + TypeScript + Vite.

## Prerequisites

- Node.js 22+
- npm
- Backend API running on `http://localhost:8080` for local dev proxy

## Quick Start

```bash
cd webui
npm ci
npm run dev
```

Open: `http://localhost:5173/ui/`

## Core Scripts

- `npm run dev` - start local Vite dev server
- `npm run build` - type-check + production build
- `npm run test` - run Vitest suite
- `npm run lint` - ESLint + design + wrapper-boundary gates
- `npm run verify:client-wrapper` - prevent direct generated-client imports outside `src/client-ts/`
- `npm run generate-client` - regenerate typed API client from `../api/openapi.yaml`

## Design And Contracts

- Design contract: `webui/DESIGN.md`
- UI contract gate script: `../scripts/check-ui-contract.sh`
- API contract source: `../api/openapi.yaml`

## Project Layout

- `src/` - production WebUI code
- `tests/` - integration/contract-focused UI tests
- `scripts/` - local verification helpers
- `dist/` - Vite build output (used for backend embed)

## Security Note

Auth tokens are stored in `sessionStorage` (not `localStorage`) to avoid
persistence across browser restarts. Clearing auth state removes stored token
and auth headers.
