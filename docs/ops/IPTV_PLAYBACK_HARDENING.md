# IPTV playback hardening change contract

Baseline: origin/main 2dde9618c61d81be7c8a2485031cbb9323df3252. Integration base: c6ef849306d68b5ed1f22f4676ffaf6c4bd4f2b3 after #1067 landed during verification.

Fixed: HLS copy playback with 10 s provider segments loses its accumulated reserve because server headroom is capped at 12 s and hls.js uses a fixed 12 s target. Explicit audio-copy profiles are incorrectly traced as codec AAC. Android starts/resumes live playback with 1/3.5 s despite pinned playback speed.
Improved: Size live startup buffering and latency from the advertised transport cadence; retain verified copy-window reserve. Integrate the existing Android cushion implementation against current Media3 1.10.0.
New: No API, provider-import capability, deployment, or native iOS decoder change.
Removed: No playback path or codec support.
Unchanged: Short-segment tuning, VOD fast start, DVR seeks, 1.0x passthrough playback, shared ingest, provider credentials, source-limited GOP timing. Provider-HLS input support remains a separate feature.
Risks: Long-cadence sources can require more startup buffering and greater live latency. Memory limits and finite timeout must remain effective. Stale player events/timers must not start a replaced or unmounted session. AAC transcode diagnostics must continue to report AAC; decoder capability probing must continue to advertise supported AC-3.
Acceptance: Negative controls fail on the baseline for long-cadence headroom/gate, copied-audio trace, and Android cold-start/resume; targeted tests and clean committed make ci-pr/pre-push pass; required GitHub gates pass before merge. End-to-end device improvement is not claimed without an authorized staging/device run.
Exit condition: No temporary playback implementation. The cushion implementation is now integrated by #1067; this PR retains its additional actual-Media3 regression coverage.

## Test matrix

- HLS backend: target cadence 1/2/3/6/10/20 s; fresh two-segment and established/sliding windows; copy/transcode; native iOS/Safari/Android/Fire TV/browser families; video/audio rendition offsets agree; VOD/master unchanged.
- React: StrictMode off/on; mount and parent/child startup effects; rerender preserves live engine; replacement executor/player invalidates prior callbacks; unmount/teardown clears timers/listeners; short/long cadence, fragmented buffered ranges, VOD, finite timeout; no drift-changing speed.
- Audio: explicit copy (including video transcode), explicit AAC transcode/audio-only transcode, legacy unspecified profile; compare finalized prediction to actual argv-derived plan.
- Android: first short segment, stalled resume, threshold boundary, small target, byte budget exhaustion, playback-speed scaling, recording delegation, onPrepared/tracks/stop/release and every current LoadControl method forwarded. Cold zap, warm join, AAC/AC-3, channel changes and recovery on real hardware remain device-validation cases.

## Existing PR inventory

- #1067: its two applicable cushion commits were ported in isolation initially. The original PR merged during verification; the integration merge retains the byte-identical production implementation and the additional actual-Media3 tests.
- #1068: main already advertises actual AC-3 decoder capabilities; branch includes unrelated trace/audio experiments. Do not merge its full tip or remove 5.1 support.
- #1058: unrelated telemetry contract/branch integration remains outside this fix.
- #1087: admission/relay building blocks are explicitly not wired into runtime; unrelated feature, no playback-fix evidence.

## Verified implementation evidence

- Backend negative controls: long-cadence playlist rewriting emitted only 12 s headroom instead of the verified 20/40 s reserve; explicit audio-copy execution produced `audioCodec:aac!=copy`. Both regression tests failed before their fixes, then passed.
- Web negative controls: both StrictMode variants lacked cadence adaptation; a disjoint future buffer range triggered premature play. These failed before the fixes.
- Android negative control: bypassing the wrapper and using the real Media3 1.10.0 `DefaultLoadControl.shouldStartPlayback` caused the new lifecycle regression to fail (`expected false, actual true`). The original source was restored immediately afterward. This is a player policy test, not a device test.
- `go vet` and `go test -race` passed for `internal/pipeline/api`, `internal/domain/session/manager`, and `internal/domain/session/model` (exit 0).
- Player suite: 63 test files, 550 tests passed (exit 0); TypeScript check passed (exit 0). Lifecycle coverage includes StrictMode on/off, stable rerender, executor replacement, captured stale callbacks, unmount, parent/child effects, finite repeated-poll deadline, VOD, and disjoint ranges.
- Android: all 174 unit tests passed and the staging debug APK built successfully (exit 0). The targeted nine tests exercise Media3 1.10.0.
- `make ci-pr` passed on clean committed head 5727cda1d (exit 0, `PR gate unit tests passed`, `PR gate bundle passed`). Six Linux Chromium browser-smoke tests, including real hls.js advancing video frames, passed in GitHub CI. The combined integration head must pass the same gates after merging #1067.
- Native iOS decoding, AirPlay, Fire TV HDMI lip sync, and device cold-zap/soak are not tested by these unit checks. Deployment and device playback are separate operator-authorized actions.

The implementation increases startup reserve and live offset for long-cadence copy streams. It does not reduce the provider GOP interval, promise a shorter cold start, or guarantee immunity to an outage longer than the buffered media.
