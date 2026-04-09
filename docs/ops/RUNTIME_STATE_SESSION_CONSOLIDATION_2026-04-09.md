# Runtime, State, Artifact, and Session Consolidation 2026-04-09

**Intent:** This milestone consolidated runtime truth, state truth, artifact truth, and session truth. The goal was not more architecture, but more honest and more robust product behavior.

## Scope / Out of Scope

In scope:

- make runtime dependencies fail early and visibly
- make runtime, backup guidance, and `storage verify --all` share one state inventory
- separate `sessions/` and `recordings/` semantically
- make browser session loss after restart recoverable or clearly terminal

Out of scope:

- no multi-node or horizontal-scaling work
- no persistent auth session store
- no split-origin support for WebUI and backend
- no new volume topology or infrastructure-driven HLS root split
- no Vercel backend target

| Area | Before | Now | Not changed |
| --- | --- | --- | --- |
| Runtime truth | Startup and readiness did not consistently treat every writable runtime path as a hard dependency. The process could come up and fail later in less obvious ways. | Startup now validates `dataDir`, `store.path`, `HLS.Root`, and recording roots as real runtime dependencies. Readiness reports missing or unwritable runtime paths as unhealthy instead of informational. | xg2g remains a long-running single-node daemon. This milestone did not introduce a new platform model or orchestration layer. |
| State truth | Runtime, backup guidance, and `storage verify --all` described different subsets of state. JSON-backed state could drift outside the operator model. | A central storage inventory now defines durable, operational, reconstructable, transient, and materialized paths. Runtime-adjacent docs and `storage verify --all` use the same inventory. | This did not convert caches or runtime artifacts into authoritative product state. Backup scope remains narrower than “everything on disk.” |
| Artifact truth | `sessions/` and `recordings/` lived under one vague HLS mental model, even though they have different lifetimes and operational meaning. | HLS path layout is now centralized and explicitly classified: `sessions/` is transient live runtime, `recordings/` is materialized recording output with reuse and eviction semantics. | `HLS.Root` remains a single root. No new volumes, no new env flags, and no physical root split were introduced here. |
| Session truth | After backend restart or lost cookie state, the player could surface a generic authentication failure for what was really a local session loss. | The player now treats a session-path `401` as potentially recoverable: it re-mints the session cookie once, retries once, and then lands in the correct terminal state such as `sessionExpired` or `sessionNotFound` instead of a false global auth alarm. | Server-side auth session storage remains in memory. Active sessions can still be lost across restart; this milestone improves the client behavior, not the storage model. |

## Operator Impact

Operators should now expect `readyz` to turn unhealthy when `store.path`, `HLS.Root`, or recording roots are missing or unwritable instead of seeing a false green startup. Backup guidance and `storage verify --all` now cover the actual state inventory rather than a smaller historical subset. Under `HLS.Root`, `sessions/` is explicitly transient runtime state while `recordings/` is materialized output. In the browser, a backend restart now leads to controlled session recovery or a clear terminal player state instead of a misleading global auth failure.

## Verification Checklist

Run these commands to verify the milestone end-to-end:

```bash
go test ./backend/internal/health ./backend/internal/api ./backend/internal/daemon
go test ./backend/internal/storageinventory ./backend/cmd/daemon
go test ./backend/internal/platform/paths ./backend/internal/control/recordings ./backend/internal/control/vod ./backend/internal/pipeline/api ./backend/internal/infra/media/ffmpeg ./backend/cmd/daemon
npm --prefix frontend/webui test -- --run src/features/player/components/V3Player.serviceRef.test.tsx
npm --prefix frontend/webui run type-check
```

Operational note: `./backend/cmd/daemon` appears in more than one verification command because both startup/runtime wiring and storage verification terminate there.

## Known Follow-Ups

- optional: add a heartbeat-specific red-path UI test for the same session-recovery state machine
- optional: clean up jsdom relative-URL noise around `sendStopIntent` in player tests

These are quality follow-ups, not blockers for the milestone.
