# Log Hygiene Ticket (WebUI Tests)

Date: 2026-02-27

## Goal

Reduce expected, non-actionable stderr/warn output in WebUI tests without globally muting console output.

## Policy

- No global silencing in `tests/setupTests.ts`.
- Suppression must be test-file opt-in with explicit expected patterns.
- Unexpected console output must remain visible.

## Implemented

- Added opt-in helper:
  - `frontend/webui/tests/helpers/consoleNoise.ts`
- Applied targeted suppression in migrated live-flow tests:
  - `frontend/webui/tests/V3Player.seal.test.tsx`
  - `frontend/webui/src/components/V3Player.serviceRef.test.tsx`
  - `frontend/webui/tests/contracts/v3player.intent-keys.contract.test.tsx`
- Applied targeted suppression in remaining noisy V3Player suites:
  - `frontend/webui/tests/V3Player.contract.test.tsx`
  - `frontend/webui/tests/V3Player.errors.test.tsx`

Suppressed patterns (expected noise only):

- `HLS playback engine not available`
- `Failed to stop v3 session`
- `Failed to parse URL from /api/v3/intents`
- `PlayerError: player.sessionFailed: SESSION_GONE: recording_deleted`
- `[V3Player][Heartbeat] Session expired (410)`

## Follow-up Candidates

- `frontend/webui/tests/contracts/v3player.failclosed.test.tsx` produced no additional stderr noise in targeted run, so no allowlist was added.
- Optional product/test harness improvement:
  - ensure teardown stop-intent uses absolute URL in jsdom tests to eliminate `/api/v3/intents` relative URL warnings at source.
