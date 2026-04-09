# WebUI Contract Consumption Index (SPEC_UI_CONSUMPTION.md)

This document governs the surface area of backend responses consumed by the WebUI. Any consumption of fields not marked as **Normative** or **Legacy** is strictly forbidden.

## Governance Categories

| Category | Description |
| :--- | :--- |
| **Normative** | Authority truth. UI must use these fields exclusively. |
| **Legacy** | Deprecated fields supported for N-1 compatibility. Avoid in new code. |
| **Forbidden** | Internal or bypass fields (e.g., `outputs[]`). UI must NEVER read these. |
| **Telemetry** | Trace IDs and error codes for operator observability only. |

---

## 1. Playback & Sessions

### `GET /api/v3/recordings/:id/playbackinfo`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `decision.mode` | Normative | `V3Player` | Selection gate (direct_play, transcode, deny) |
| `decision.selectedOutputUrl` | Normative | `V3Player` | **Authority URL** for playback |
| `decision.selectedOutputKind` | Normative | `V3Player` | Engine selection (hls vs mp4) |
| `decision.outputs[]` | **Forbidden** | `V3Player` | Direct selection from outputs is prohibited |
| `url` | Legacy | `V3Player` | Fallback for P3-x backends |
| `mode` | Legacy | `V3Player` | Fallback for P3-x (hls, direct_mp4) |
| `requestId` | Telemetry | `V3Player` | Diagnostic trace display |
| `isSeekable` | Normative | `V3Player` | Authoritative seekability gate. Missing values must fail closed instead of enabling seek/resume affordances. |
| `durationSeconds` | Normative | `V3Player` | Authoritative playback duration truth in the only supported API unit. |
| `durationMs` | **Forbidden** | `V3Player` | Removed legacy duration field. UI must not read or reconstruct it. |
| `resume` | Normative | `V3Player` | Populates resume overlay state |

### `POST /api/v3/intents`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `sessionId` | Normative | `V3Player` | Global session tracking |
| `status` | Telemetry | `V3Player` | Accepted vs idempotent replay. UI must not derive playback state from this alone. |
| `requestId` | Telemetry | `V3Player` | Trace Id initialization |

Request governance:
- `V3Player` MUST send the live attestation token via root-level `playbackDecisionToken`.
- `params.playback_decision_token` and `params.playback_decision_id` are deprecated compatibility aliases and forbidden in new WebUI code.

### `POST /api/v3/live/stream-info` (`503 application/problem+json`)

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `type` | Normative | `V3Player`, `EPG` | Primary live problem discriminator. Stable values are `/problems/live/scan_unavailable`, `/problems/live/missing_scan_truth`, `/problems/live/partial_truth`, `/problems/live/inactive_event_feed`, `/problems/live/failed_scan_truth`. |
| `code` | Normative | `V3Player` | Broad family only (`SCAN_UNAVAILABLE` or `UNAVAILABLE`). UI must not derive subtype from `code` alone. |
| `Retry-After` | Normative | `V3Player` | Retry/backoff gating for manual or timed retry. |
| `retryAfterSeconds` | Normative | `V3Player` | JSON mirror of `Retry-After` for raw fetch paths. |
| `truthState` | Normative | `V3Player` | Stable degraded-state classifier (`unverified`, `partial`, `failed`, `inactive_event_feed`). |
| `truthReason` | Normative | `V3Player` | Stable UX reason code. |
| `title` | Legacy | `V3Player`, `EPG` | Human fallback only. Clients must not branch on prose. |
| `detail` | Legacy | `V3Player`, `EPG` | Human fallback only. Clients must not branch on prose. |
| `truthOrigin` | Telemetry | `V3Player` | Diagnostic provenance only. |
| `problemFlags` | Telemetry | `V3Player` | Diagnostic-only flags; do not use as the primary decision gate. |
| `requestId` | Telemetry | `V3Player`, `EPG` | Correlates operator reports and backend traces. |

### `POST /api/v3/live/stream-info` (`200 application/json`)

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `decision.mode` | Normative | `V3Player`, `EPG` | Authoritative playback-mode truth. `EPG` channel badges may classify only from this field. |
| `decision.selectedOutputUrl` | Normative | `V3Player` | Authoritative live playback URL. |
| `decision.selectedOutputKind` | Normative | `V3Player` | Engine selection for live playback. |
| `videoCodec` | Normative | `EPG` | Optional codec badge detail. |
| `audioCodec` | Normative | `EPG` | Optional codec badge detail. |
| `mode` | Legacy | `V3Player` | Compatibility fallback only. `EPG` must not branch on it. |
| `decision.trace.*` | **Forbidden** | `EPG` | Diagnostic trace only. Channel badges must not derive product semantics from trace internals. |

### `GET /api/v3/sessions/:id`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `state` | Normative | `V3Player` | Lifecycle management (READY, FAILED) |
| `playbackUrl` | Normative | `V3Player` | Live engine source |
| `heartbeatIntervalSeconds` | Normative | `V3Player` | Canonical server heartbeat cadence for lease renewal. No alias fields are allowed. |
| `leaseExpiresAt` | Normative | `V3Player` | Observed session lease snapshot for countdown/display. |
| `reason`, `reasonDetail` | Normative | `V3Player` | Terminal/degraded session explanation when the state leaves the serving path. |

### `POST /api/v3/sessions/:id/heartbeat`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `acknowledged` | Normative | `V3Player` | Confirms the heartbeat was accepted for the addressed session. |
| `leaseExpiresAt` | Normative | `V3Player` | Renewed lease expiry after a successful heartbeat. |
| `sessionId` | Normative | `V3Player` | Heartbeat acknowledgement identity. Must match the tracked session. |

---

## 2. Timer & DVR Management

### `GET /api/v3/recordings`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `recordings[].status` | Normative | `RecordingsList` | Authoritative coarse-grained recording truth. Stable values are `scheduled`, `recording`, `completed`, `failed`, `unknown`. `unknown` means no confirmed recording truth is available; UI may treat that as a truth gap, but must not infer hidden sub-causes. |
| `recordings[].title` | Normative | `RecordingsList` | Display title only. UI must not infer recording state from legacy title markers such as `[REC]`, `[WAIT]`, or `[ERROR]`. |
| `recordings[].resume` | Normative | `RecordingsList` | Resume progress overlay and watched-state utility labels. |
| `recordings[].durationSeconds` | Normative | `RecordingsList`, `V3Player` | Duration truth for playback affordances and formatting. |
| `recordings[].length` | Legacy | `RecordingsList` | Human fallback string only. Prefer `durationSeconds` when present. |
| `recordings[].serviceRef` | Legacy | `RecordingsList` | Diagnostic/compatibility field only. Do not treat it as UI identity. |

### `GET /api/v3/timers`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `timerId` | Normative | `Timers`, `EditTimerDialog` | Identity |
| `state` | Normative | `Timers` | Status badge rendering |
| `begin`, `end` | Normative | `Timers` | Schedule display |
| `name`, `description` | Normative | `Timers` | Metadata display |

### `GET /api/v3/epg`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `[]` | Normative | `EPG` | The response is a bare array. UI must not expect or accept an `{ items: ... }` wrapper. |
| `[].serviceRef` | Normative | `EPG` | Channel identity for grouping and playback handoff. |
| `[].start`, `[].end` | Normative | `EPG` | Event time window in unix seconds. |
| `[].title` | Normative | `EPG` | Primary programme label. |
| `[].desc` | Normative | `EPG` | Secondary programme detail when present. |
| `[].id`, `[].duration` | Telemetry | `EPG` | Optional diagnostics only; current UI must not branch on them. |

### `POST /api/v3/timers/preview`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `canSchedule` | Normative | `EditTimerDialog` | Save-button gating |
| `conflicts[]` | Normative | `EditTimerDialog` | Conflict warning display |

---

## 3. System & Configuration

### `GET /api/v3/system/config`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `openWebIF.streamPort` | Normative | `Config` | Connection configuration |
| `epg.*` | Normative | `Config` | EPG policy management |
| `streaming.deliveryPolicy` | Normative | `Settings` | Read-only policy status |
| `household.pinConfigured` | Normative | `Settings`, `Navigation`, `HouseholdProfilesProvider` | PIN-gated profile switching, child logout guard, household settings copy |

### `GET /api/v3/system/health`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `status` | Normative | `Files`, `Settings` | Health badge |
| `epg.status` | Normative | `Files` | EPG readiness check |
| `version` | Telemetry | `Settings` | Footer information |

---

## 4. Forbidden Fields (Mechanical Gates)

The following fields must be caught by automated contract scans:

- `outputs[]` in any PlaybackInfo response (Use `selectedOutputUrl`).
- `profiles[]` in Config responses (Obsolete: Universal Policy only).
- `transcodeParams` in Session responses (Backend managed only).
