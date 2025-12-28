# V3 Session Lifecycle (FSM)

## State Semantics
- STARTING: Lease acquired, worker init and FFmpeg start/probe in progress.
- PRIMING: FFmpeg running, HLS artifacts being produced; not yet playable.
- READY: Playable guarantee (playlist exists and at least one segment exists, atomically published).
- DRAINING: Stop requested; existing HLS assets remain serveable until drain_timeout.
- STOPPING: Teardown in progress; no new assets are produced.
- STOPPED: Normal end after READY (or clean teardown).
- FAILED: Terminal error; reason_class set.
- CANCELLED: User abort before READY or forced abort.
- NEW (internal only): Transient pre-start state; should not be user-visible for long.

## Admission Policy
- Lease unavailable: return HTTP 409 with reason_class=lease_busy; no session created.
- Control plane draining: return HTTP 503 (Retry-After recommended); no session created.

## Non-Negotiable Guards
1) Admission guard: no session is created without a successful lease acquisition.
2) READY guard: READY may only be set after playlist + at least one segment exist and are atomically published.
3) Timeout guard: each phase has a deadline (STARTING, PRIMING, DRAINING/STOPPING).
4) Drain guard: while draining, reject new starts and terminate active sessions cleanly.

## Transition Table
| Current  | Event               | Guard                                           | Next      | Notes |
|----------|---------------------|-------------------------------------------------|-----------|-------|
| STARTING | FfmpegStarted        | -                                               | PRIMING   | Worker/FFmpeg started. |
| STARTING | StartTimeout         | -                                               | FAILED    | reason_class=tune_failed or ffmpeg_failed. |
| PRIMING  | FirstSegmentReady    | playlist && segment && atomic_publish           | READY     | READY is a hard playable guarantee. |
| PRIMING  | PrimingTimeout       | -                                               | FAILED    | reason_class=playlist_timeout or packager_failed. |
| READY    | StopRequested        | -                                               | DRAINING  | Begin graceful stop. |
| DRAINING | DrainTimeout         | -                                               | STOPPING  | Force teardown. |
| DRAINING | StopComplete         | -                                               | STOPPED   | Clean end. |
| STOPPING | TeardownComplete     | -                                               | STOPPED   | Final terminal state. |
| ANY      | WorkerError          | map_error()                                     | FAILED    | Single source of error mapping. |
| ANY      | ClientCancel         | -                                               | CANCELLED | User-initiated stop or forced abort. |

## Error Mapping
- map_error() is the single source of truth for reason_class and HTTP mapping.
- No ad-hoc reason strings in handlers.

## Client handling rules (normative)
This section defines required client behavior for `/api/v3` session lifecycle semantics. Clients must follow these rules to remain compatible across versions.

### Unknown states
Clients MUST treat any unknown `state` value as not ready yet and continue polling (or present a generic "starting" UI).
Clients MUST NOT fail hard solely due to an unknown `state` value.

Rationale: the server may introduce additional intermediate states in future versions without breaking compatibility.

### Admission rejections (409 LEASE_BUSY)
`POST /api/v3/intents` MAY return 409 Conflict with error code `LEASE_BUSY` (and optional `Retry-After`) when capacity is exhausted.

Clients MUST assume that no session was created when receiving 409.
Clients SHOULD respect `Retry-After` (seconds) when present before retrying.
Clients MUST NOT attempt to `GET /api/v3/sessions/{id}` based on any locally fabricated or previous `sessionId` in response to 409.

### READY guarantee (playable contract)
When `GET /api/v3/sessions/{id}` returns `state=READY`, playback MUST be immediately possible.

READY implies:
- the playlist (`index.m3u8`) exists and is served successfully, AND
- at least one media segment exists and is available, AND
- HLS artifacts have been atomically published (no partial playlist exposure).

Clients SHOULD begin playback immediately upon observing `READY`.
If a client observes `READY` but cannot fetch the playlist or first segment, clients SHOULD treat this as a server regression (surface an actionable error and optionally retry), not as normal startup behavior.

### Polling guidance
Clients SHOULD poll `GET /api/v3/sessions/{id}` until `READY` or a terminal state (`FAILED`, `CANCELLED`, `STOPPED`) is reached.
Clients MUST treat `STARTING` and `PRIMING` as expected intermediate states and MUST NOT interpret them as playback buffering conditions.

Buffering UX MUST be driven by media element events (e.g., `waiting/stalled`) after playback begins, not by session state alone.

### Terminal states (polling termination)
When a session enters a terminal state (`FAILED`, `CANCELLED`, or `STOPPED`), clients MUST stop polling
`GET /api/v3/sessions/{id}` and MUST NOT expect further state transitions.

Any subsequent client action requires creating a new session via `POST /api/v3/intents`.

## Terminal State Semantics
- STOPPED: normal completion after READY (or clean teardown).
- CANCELLED: user abort before READY or forced abort.
- FAILED: error with reason_class.
