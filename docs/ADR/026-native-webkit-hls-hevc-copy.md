# ADR-026: Native WebKit HLS (fMP4/hvc1 copy) for HEVC live sources on Safari

**Status:** Accepted (staged behind a flag, default off)
**Date:** 2026-06-16
**Triggers:** RTL UHD (4K HEVC Main10 HLG @50fps) unplayable in the browser on a MacBook Pro M4

## Context

Some live DVB sources are **HEVC Main 10, 10-bit, 3840×2160, 50 fps, BT.2020 + HLG HDR** (e.g. RTL UHD). After the ingest fixes that let such a source reach the player at all (deeper live-copy probe budget; dropping the spoofed `VLC` user-agent that tripped the OSCam stream-relay), playback still failed in the browser with `R_CLIENT_STOP` / "hls.js stall recovery failed".

Root cause: the WebUI's MSE migration (`shouldPreferNativeWebKitHls`) routes desktop Safari (ManagedMediaSource + AV1-10bit) to **hls.js/MSE**, and hls.js/MSE cannot sustain 4K@50 HEVC Main10/HLG. For a Safari **browser** client the backend resolves `ProfileSafari` → `Container=mpegts`, video **copy**; native Safari HLS does not play HEVC in MPEG-TS (Apple requires fMP4/`hvc1` for HEVC).

Research (hls.js docs/issues; Apple HLS authoring; Apple-silicon media-engine specs): hls.js itself recommends **native HLS** in browsers with ManagedMediaSource; Safari decodes HEVC + HLG natively; the M4 media engine hardware-decodes HEVC Main10/HLG up to 2160p60. The correct path is therefore **native WebKit HLS + fMP4/hvc1 while keeping video COPY** — no transcode, no quality loss, HW-decoded on the client.

## Decision

For an **HEVC live source** on a **Safari client that can decode HEVC and exposes a native HLS engine**, deliver the copy as **fMP4/hvc1** and have the client play it via **native WebKit HLS** instead of hls.js/MSE. This is a deliberate, **HEVC-only exception** to the MSE/hls.js default (see ADR/notes on the MSE migration); H.264 keeps the hls.js/MSE path (app-owned buffer / seek).

Gated behind a per-device experiment flag (`XG2G_NATIVE_HEVC_SAFARI`, default off, kill switch `…_KILL`, query `?xg2g_native_hevc=1`), so it is opt-in for device verification before any broader rollout, and reversible without a redeploy.

## Implementation (reuses existing machinery)

- **Frontend** (`shouldPreferNativeWebKitHls` in `utils/playerHelpers.ts`, at capability-probe time): when the flag is on and the client can decode HEVC Main 10 (`canPlayType hvc1/hev1`), prefer the **native** WebKit HLS engine — *before* `/live/stream-info`. This sets `preferredHlsEngine='native'` from the probe, so the same caps go to both the decision and the intent. The flag lives in `utils/nativeHevcExperiment.ts`. The decision is engine-decided up front rather than mutated after the decision (see Safety).
- **Backend** (`clientpolicy.ApplyStartPackagingPolicy`): for a Safari-native client whose resolved spec is HEVC video-**copy** in `mpegts` AND `preferred_hls_engine=='native'`, force `Container=fmp4` **and** pin `VideoCodec="hevc"`. The codec pin is required because the copy path defaults `VideoCodec=""` → `planCodec` resolves `"h264"` → `appendLiveVideoContainerTags` would skip `-tag:v hvc1` and FFmpeg writes an `hev1` fMP4 that native Safari HLS refuses ("HEVC is not hvc1"). It stays a COPY (`Name!=""` keeps `usesLegacyCPUDefaults` false; `TranscodeVideo` untouched → `buildCopyVideoArgs`/`-c:v copy`). `appendLiveHLSArgs` then emits the fMP4 init + `.m4s` segments.
- No change to the ffmpeg arg builder, the native attach path, or `safariFamilyContainer` (browser default stays mpegts).

## Safety / reversibility

- **Token-consistent (NOT post-decision):** the decision token's `claims.CapHash` is checked against `hashV3Capabilities(intent.client)` at intent time, and that hash **does** cover `preferredHlsEngine` — mutating the engine *after* `/live/stream-info` was rejected with `CLAIM_MISMATCH`. Therefore the engine is chosen at probe time (before the decision), so `/live/stream-info` and the intent carry identical capabilities and the token validates.
- **No silent change when off:** the engine flip only happens when the flag is on; the backend container flip is additionally gated on `preferred_hls_engine=='native'` + HEVC source. Flag-off traffic is byte-for-byte unchanged.
- **HEVC-only packaging:** under the flag a Safari client uses native HLS for *all* live sources (H.264 included — it still plays via native Safari HLS), but the backend only flips the **container** to fMP4/hvc1 for **HEVC** sources; H.264 stays MPEG-TS. The MSE/seek benefit is traded away only on the flagged device.
- **Fallback:** the existing unconditional code-3 (decode) playback fallback covers a client decode failure from any engine; the flag/kill switch is the per-device revert. A dedicated native-HEVC fallback is a follow-up for broader rollout.

## Open / risks

- The core assumption (native + fMP4/hvc1 sustains 4K@50 HLG) is proven only after the on-device M4 verification; nothing rolls out beyond the flagged device until it plays.
- `withinMaxVideo` must keep the decision at direct/copy (a Safari `maxVideo` < 2160p50 would force transcode and moot this path).
- DVB-relay copy timestamp judder is orthogonal and tracked separately.
