# WebUI source layout

Feature code lives in `features/<name>/`. Shared, cross-feature code is split by
**role, not by type** — when adding a non-feature module, place it by what it does:

- **`services/`** — modules that cross an I/O boundary: the generated-API client
  wrapper (`clientWrapper`), telemetry emission (`TelemetryService`). Anything
  that talks to the network or an external sink.
- **`lib/`** — app-aware foundations and cross-cutting primitives with no single
  feature owner: the error model/catalog (`appErrors`, `errorCatalog`,
  `httpProblem`), routing/host context (`routeContext`, `hostBridge`,
  `browserNavigation`), UI scale/surface (`uiScale`, `uiSurface`).
- **`utils/`** — small, dependency-light, portable helpers that could live in any
  project: `text`, `logging`, `tokenStorage`.
- **`components/`** — shared presentational components (PascalCase, with
  co-located `*.module.css` and `*.test.tsx`).
- **`hooks/`** / **`context/`** / **`types/`** — shared React hooks, context
  providers, and shared TypeScript types.
- **`client-ts/`** — generated OpenAPI client. **Never hand-edit**; regenerate.

Rule of thumb for the `lib` vs `utils` boundary: if it imports app/domain or
framework modules, it's `lib`; if it's a pure, portable helper, it's `utils`.
