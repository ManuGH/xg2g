# Safari/iOS Playback Audit

Last updated: 2026-02-27

## Goal

Hard proof that Safari playback is capability-driven and fail-closed:

- no UA-sniffing policy in WebUI
- native HLS preferred when available
- hls.js used only when explicitly supported (MSE/MMS path)
- autoplay/gesture behavior tested and documented for Live + VOD

## Source-of-Truth Rules

1. Backend decides playback mode (`native_hls | hlsjs | direct_mp4 | transcode | deny`).
2. WebUI only maps server mode to engine bridge and validates local engine availability.
3. Missing mode, unsupported mode, or unavailable engine => fail-closed.

## Capability Matrix

| Platform | Native HLS (`video.canPlayType`) | hls.js path (`Hls.isSupported`) | Policy |
| --- | --- | --- | --- |
| iOS Safari (< 17.1) | Usually yes | No/unsupported | Prefer `native_hls`; fail-closed if backend requests hls.js-only mode |
| iOS Safari (17.1+) | Yes | Possible via MMS/MSE depending on runtime | Prefer `native_hls`; allow `hlsjs` only when `Hls.isSupported()` is true |
| iOS WKWebView | Runtime-dependent | Runtime-dependent | Same capability-driven rule; no UA branch |
| macOS Safari | Yes | Runtime-dependent | Prefer backend mode; `hlsjs` requires explicit support |
| Chromium/Firefox baseline | Usually no native m3u8 | Usually yes (MSE) | `hlsjs` path valid when supported |

## Failure Taxonomy

- `MODE_MISSING`: backend response lacks `mode`.
- `MODE_INVALID`: mode not in contract enum.
- `ENGINE_UNAVAILABLE`: mode maps to engine not available in current runtime.
- `AUTOPLAY_BLOCKED`: `play()` rejected due to platform policy.
- `DENIED_BY_BACKEND`: backend returns `mode=deny` and reason.

## Governance Gates

### Gate W — No UA Sniffing (mechanical)

- Script: `webui/scripts/no-ua-sniffing.mjs`
- Fails on `navigator.userAgent/platform/vendor/appVersion` in runtime `webui/src/**`.
- Tests/stories/generated client are excluded.
- Allowlist comment for telemetry-only tags: `ua-telemetry-only`.

### Gate X — Engine Availability Explicit (contract)

- Bridge contract enforces:
  - `native_hls` => `native`
  - `hlsjs|transcode` => `hlsjs`
  - `direct_mp4|deny` => no HLS engine
- Additional availability check:
  - if mapped engine is not available in measured capabilities, fail-closed.
- Contract test: `webui/tests/contracts/v3player.mode-bridge.test.ts`.

### Gate Y — HLS MIME + Playlist Sanity (server-side)

Required checks (Safari focused):

- `.m3u8` => `application/vnd.apple.mpegurl` (or `application/x-mpegURL`)
- `.ts` => `video/mp2t`
- `.m4s`/fMP4 => `video/mp4` (or `video/iso.segment` if pipeline changes)
- playlist tags are internally consistent (`TARGETDURATION`, segment tags, container mode)

Status: implemented.

Proof tests:

- `internal/control/http/v3/handlers_hls_test.go`:
  - `TestHLSHandlers_GateY_PlaylistAndMediaSanity`
- `internal/control/http/v3/contract_v3_test.go`:
  - `TestV3Contract_HLS`

### Gate Z — Manual Repro Checklist (release)

Required manual checks on real devices:

1. iOS Safari Live start (with user gesture) succeeds.
2. iOS Safari VOD start + seek/resume behavior is correct.
3. Autoplay without gesture fails gracefully with user-visible message.
4. `mode=deny` path remains fail-closed (no `/intents` follow-up).
5. iOS 17.1+ scenario with hls.js availability behaves capability-driven (no UA branch).

Status: implemented as release procedure.

Runbook:

- `docs/ops/SAFARI_REPRO_RUN.md`
- `docs/release/RELEASE_TEMPLATE.md` (Gate Z release section)

## Autoplay/Inline Checklist

- `playsInline` applied on `<video>`.
- muted-start behavior documented and tested.
- gesture-gated start path available for both Live and VOD.
- blocked `play()` transitions to actionable UI state (not spinner deadlock).

## Notes

- Native HLS remains first preference for iOS due to platform integration and stability.
- hls.js-on-iOS is treated as opportunistic capability, never as a UA-forced path.
