# Startup Validation 2026-03-17

## Scope
- Commit: `bd48c781`
- Version on host during validation: `v3.1.7-223-gbd48c781`
- Goal: validate that earlier live HLS readiness translates into earlier real browser consume, not only earlier internal `READY`

## Change Under Test
- Lower default `hls.readySegments` from `3` to `1`
- Apply `-hls_init_time 2` to the generic live HLS path when `hls_time` is larger
- Keep the `safari_dirty` startup path unchanged at `2/1`

## Method
- Live host before/after comparison
- Channels:
  - `ORF1 HD`
  - `PULS 24 HD`
  - `ZDF HD`
- Runs per channel:
  - `3` before
  - `3` after
- Client model:
  - Safari-like `hlsjs` client
  - tokenized HLS playback URL
  - `30s` post-start soak
- Structured markers per run:
  - `intent accepted`
  - `ffmpeg first segment write observed`
  - `playlist ready - transitioning to READY state`
  - first `index.m3u8` request
  - first `seg_*` request

Raw datasets:
- `/tmp/xg2g_startup_before.jsonl`
- `/tmp/xg2g_startup_after.jsonl`

## Result

### Overall
- `intent -> first segment`: median `13s -> 11s`
- `intent -> READY`: median `21s -> 12s`
- `intent -> first manifest`: median `21s -> 12s`
- `first segment -> READY`: median `8s -> 1s`
- `READY -> first manifest`: median `0s -> 0s`
- `30s` soak health: `9/9` before clean, `9/9` after clean

### Per Channel
- `ORF1 HD`
  - `intent -> READY`: `20s -> 11s`
  - `intent -> first manifest`: `20s -> 11s`
- `PULS 24 HD`
  - `intent -> READY`: `21s -> 12s`
  - `intent -> first manifest`: `21s -> 12s`
- `ZDF HD`
  - `intent -> READY`: `21s -> 13s`
  - `intent -> first manifest`: `21s -> 13s`
  - previous outlier at `29s` to `READY` did not reappear after deploy

## Interpretation
- The main gain came from earlier readiness release, not from dramatically faster first segment production.
- The key runtime proof is that `intent -> first manifest` dropped by the same order as `intent -> READY`.
- This means the earlier release is visible in the browser and is not only an internal state change.
- The change removed most of the artificial wait between "first playable material exists" and "session is released to the client".
- For `-c:v copy`, first-segment timing remains source-keyframe-bound. `-hls_init_time 2` is best effort, not a hard startup SLA.

## Operational Note
- One after-run hit `ADMISSION_NO_TUNERS` during the measurement window.
- That run was excluded as infrastructure noise, the service was restarted, and the missing run was repeated successfully.
- Final comparison remained symmetric at `9` runs before vs `9` runs after.

## Decision
- `bd48c781` is runtime-validated.
- The earlier `READY` contract is now confirmed as productively useful in practice for the tested live HLS flows.
- No early-stability regression was observed in the measured `30s` post-start window.
