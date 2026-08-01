# Safari/iOS Manual Repro Run (Gate Z)

Last updated: 2026-02-27

## Purpose

Provide a short, repeatable release check that catches Safari/iOS playback regressions not fully covered by CI gates.

## Preconditions

- Device/OS:
  - iPhone or iPad with iOS 17.1+ (Safari)
  - macOS with Safari 17.1+
- Network:
  - stable WLAN
  - no VPN/proxy that alters media traffic
- Auth:
  - valid session/cookie (or valid bearer token)
- Test assets:
  - one stable Live channel
  - one Recording/VOD asset

## Test Matrix

### A. Live Playback (iOS Safari)

1. Open WebUI.
2. Select a Live channel.
3. Tap Play (explicit user gesture).

Expected:

- no silent autoplay start without gesture
- audio/video starts within 5 seconds
- no endless spinner
- UI flow is coherent (`preflight -> authorizing -> buffering -> playing`)

Capture on failure:

- screenshot or short screen recording
- request ID / correlation ID from response headers or problem details

### B. Recording/VOD Playback (iOS Safari)

1. Open a Recording.
2. Tap Play.
3. Pause after ~10 seconds.
4. Leave and reopen playback.
5. Trigger Resume and a seek action.

Expected:

- duration display comes from DTO and is sane
- seek UI is enabled only when `isSeekable=true`
- resume returns near the last saved position
- seek (+10s/scrub) works when seekable

Capture on failure:

- screenshot
- request ID and problem `type/code/title` if present

### C. Negative Case (Fail-Closed)

1. Trigger denied/invalid playback decision path (test toggle/token invalidation path).

Expected:

- explicit problem-aware error in UI (no silent fallback)
- playback does not start without a valid decision
- `mode=deny` path remains blocked

Capture on failure:

- screenshot
- request ID plus problem payload summary

### D. macOS Safari Spot Check

1. Repeat A (Live start) briefly.
2. Repeat B (VOD start + one seek) briefly.

Expected:

- same fail-closed and seekability semantics as iOS

## Acceptance Criteria

- Live: audio/video starts, no endless buffering, deny path handled correctly.
- VOD: duration/resume/seek behavior matches DTO truth.
- Errors: UI shows problem-aware error semantics, request IDs are captured.
- Any failure: issue filed with evidence links and request IDs.

## Evidence Attachment

For each release, record:

- tested device/OS versions
- pass/fail per case (A/B/C/D)
- request IDs for any failure
- screenshot/screen recording links

## Known Quirks

- iOS 17.1+ can expose MMS/MSE in some environments; availability can differ between real devices and simulators.
- Treat engine support as capability-driven runtime truth (`Hls.isSupported()`), not as UA policy.
