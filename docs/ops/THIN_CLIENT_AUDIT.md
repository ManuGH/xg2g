# WebUI Thin-Client Audit

Last updated: 2026-02-27

## Goal

Hard proof that WebUI acts as API client #1:

- no client-side playback decision engine
- no client-side truth calculation
- no hidden JSON contracts outside typed surfaces without explicit justification

## Scope

Audited paths:

- `webui/src/components/V3Player.tsx`
- `webui/src/features/resume/useResume.ts`
- `webui/src/components/Config.tsx`
- `webui/src/client-ts/**`
- `webui/tests/contracts/**` (contract gates)

Search patterns used:

- `fetch(`
- `.json(`
- `mode`, `transcode`, `directplay`, `directstream`, `decision`
- duration/seek/resume truth inputs

## Thin-Client Policy

Allowed client logic:

- rendering/layout/i18n
- transport invocation and display of backend reason/problem codes
- view-state transitions from backend responses/network state
- ephemeral UI state (buffering/loading/stats)

Forbidden client logic:

- client playback policy selection (direct/hls/transcode)
- client truth derivation for duration/seekability/resume clamping
- silent client-side correction of backend decisions
- hidden JSON API contracts without typed client or explicit, auditable exception

## Non-SDK Fetch Inventory

Findings from `rg "fetch\\(" webui/src`:

1. `webui/src/components/Config.tsx`
   - `/healthz` (`GET`): non-JSON liveness polling after restart. Allowed.
   - `/internal/setup/validate` (`POST` JSON): internal bootstrap endpoint not in generated SDK.
     - Marked: `raw-fetch-justified: internal setup bootstrap endpoint is not part of typed public SDK surface.`

2. `webui/src/components/V3Player.tsx`
   - `/sessions/{id}/feedback` (`POST` JSON)
   - `/intents` (`POST` JSON; stop/start)
   - `/live/stream-info` (`POST` JSON)
   - `/sessions/{id}/heartbeat` (`POST` JSON)
   - stream probes (`HEAD` to selected stream URL; non-JSON)
   - All JSON raw-fetch paths now carry `raw-fetch-justified:` tags and are auditable.

## Contract Fidelity Notes

- Duration truth remains DTO-first (`durationMs`, `isSeekable`, reasons) and guarded by Gate K.
- Live mode bridge is centralized:
  - `webui/src/components/v3playerModeBridge.ts`
  - exhaustive backend mode table
  - fail-closed default for missing/invalid/unsupported modes
- RFC7807 parsing is centralized:
  - `webui/src/lib/httpProblem.ts`
  - `V3Player` consumes `assertOkOrProblem` / `parseProblemResponse` for live/session API errors
- V3Player consumes backend mode via the bridge and throws on contract drift (no implicit fallback mode).
- V3Player validates mapped engine availability from measured capabilities and fails closed on mismatch.
- Resume persistence is gated by server seekability:
  - `useResume` now receives `isSeekable`
  - no resume-save side effects when server reports `isSeekable=false`
- Legacy client decision helper was moved out of runtime source:
  - from `webui/src/contracts/PolicyEngine.ts`
  - to `webui/tests/contracts/helpers/PolicyEngine.ts` (test-only)

## Mechanical Gates

- Gate K: `gate:no-duration-guessing` (already active)
- Gate L: `gate:no-client-decision-engine` (active)
- Gate M: `gate:no-raw-json-fetch` (active)
- Gate N: `gate:no-raw-error-text` (active)
- Gate O: `gate:no-seek-resume-guessing` (active)
- Gate P: `gate:mode-bridge` (active, table-driven contract test)
- Gate W: `gate:no-ua-sniffing` (active)
- Gate Q: `gate:no-appledouble` (active, blocks `._*` artifacts in webui)
- Gate O phase 2 proof: `gate:seekable-contract` (active, seek/resume behavior contract test)

`make gate-webui` now executes:

1. `scripts/ci_gate_webui_audit.sh`
2. `npm --prefix webui run gate:no-appledouble`
3. `npm --prefix webui run gate:no-duration-guessing`
4. `npm --prefix webui run gate:no-client-decision-engine`
5. `npm --prefix webui run gate:no-ua-sniffing`
6. `npm --prefix webui run gate:no-seek-resume-guessing`
7. `npm --prefix webui run gate:no-raw-json-fetch`
8. `npm --prefix webui run gate:no-raw-error-text`
9. `npm --prefix webui run gate:mode-bridge`
10. `npm --prefix webui run gate:seekable-contract`

## Status

- Gate L: PASS
- Gate M: PASS
- Gate N: PASS
- Gate O: PASS
- Gate P: PASS
- Gate W: PASS
- Gate Q: PASS
- Gate O phase 2 proof: PASS
- Global thin-client hardening gates: none pending in current scope
