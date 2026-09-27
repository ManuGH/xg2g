# Client Playback Telemetry

This note describes the bounded client-side playback trace uploaded by native
clients. The wire contract is `ClientPlaybackTraceBatch` in
`backend/api/openapi.yaml`.

## Android and Fire TV

The Android player records Media3 events into an in-memory ring buffer. It keeps
at most 128 events from the previous 30 seconds and uploads only after a
network load failure, dropped or late frames, a decoder error, an audio renderer
failure, or decoder recovery. Uploads are delayed briefly to include the
recovery tail and are throttled to one per five seconds. Stopping or changing
channels flushes a pending anomaly trace asynchronously.

Android uses the session path `POST /api/v3/sessions/{sessionId}/trace` because
its playback flow starts through `intents` and has no stream-preparation ID.
The request body contains monotonic event times and event types, but no channel
name, stream URL, or device identifier. The backend validates size, age, event
ordering, event/stage pairs, and session existence, then writes one structured
log event correlated by session ID. It does not persist the event window or add
per-device metric labels.

Media3 exposes segment load start/completion/failure, decoder/player errors,
rendered first frame, dropped frames, and the existing Fire TV stall watchdog.
Android does not claim demux milestones that Media3 does not expose (such as
PAT/PMT parsing or first IDR); those remain specific to the native MPEG-TS
pipeline on Apple platforms.

An upload is best effort. HTTP failure is logged locally and does not delay or
change playback. The ring buffer is memory-only and is discarded when playback
ends.
