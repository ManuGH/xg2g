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
| `isSeekable` | Normative | `V3Player` | Controls UI seek bar gating |
| `durationSeconds` | Normative | `V3Player` | Timeline bounds |
| `resume` | Normative | `V3Player` | Populates resume overlay state |

### `POST /api/v3/intents`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `sessionId` | Normative | `V3Player` | Global session tracking |
| `requestId` | Telemetry | `V3Player` | Trace Id initialization |

### `GET /api/v3/sessions/:id`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `state` | Normative | `V3Player` | Lifecycle management (READY, FAILED) |
| `playbackUrl` | Normative | `V3Player` | Live engine source |
| `heartbeat_interval` | Normative | `V3Player` | Lease management timing |
| `lease_expires_at` | Normative | `V3Player` | Display/Countdown (ADR-009) |

---

## 2. Timer & DVR Management

### `GET /api/v3/timers`

| Field | Category | Component | Usage |
| :--- | :--- | :--- | :--- |
| `timerId` | Normative | `Timers`, `EditTimerDialog` | Identity |
| `state` | Normative | `Timers` | Status badge rendering |
| `begin`, `end` | Normative | `Timers` | Schedule display |
| `name`, `description` | Normative | `Timers` | Metadata display |

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
