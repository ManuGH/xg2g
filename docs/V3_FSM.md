# V3 Session Lifecycle (FSM)

> **Normative specification** for the V3 session state machine.
> All implementations MUST adhere to this contract.

---

## State Semantics

| State | Meaning |
|-------|---------|
| **NEW** | Intent accepted, session created but not yet assigned to worker |
| **STARTING** | Lease acquired, worker and FFmpeg initialization in progress |
| **PRIMING** | FFmpeg running, HLS artifacts being produced; not yet playable |
| **READY** | Playable guarantee - playlist and segment atomically published |
| **DRAINING** | Stop requested; assets remain serveable until timeout |
| **STOPPING** | Teardown in progress; no new assets are produced |
| **STOPPED** | Normal end after READY (or clean teardown) |
| **FAILED** | Terminal error; reason code set |
| **CANCELLED** | User abort before READY or forced abort |

---

## Admission Policy

When capacity is exhausted or system is unhealthy:

- **Lease unavailable**: Return HTTP **409 Conflict** with
  `reason=LEASE_BUSY`; **no session created**
- **Control plane draining**: Return HTTP **503 Service Unavailable** with
  `Retry-After` header; **no session created**

---

## Non-Negotiable Guards

These invariants MUST be enforced by all implementations:

1. **Admission Guard**: No session is created without successful lease acquisition
2. **READY Guard**: READY state may only be set after playlist + segment
   exist and are atomically published
3. **Timeout Guard**: Each phase has a deadline (STARTING, PRIMING,
   DRAINING/STOPPING)
4. **Drain Guard**: While draining, reject new starts and terminate active
   sessions cleanly

---

## FSM Transition Table

| Current State | Event | Guard | Next State | Reason Code | Notes |
|---------------|-------|-------|------------|-------------|-------|
| NEW | LeaseAcquired | - | STARTING | R_NONE | Worker assigned |
| STARTING | FfmpegStarted | - | PRIMING | R_NONE | Worker/FFmpeg started |
| STARTING | StartTimeout | - | FAILED | R_TUNE_FAILED / R_FFMPEG_START_FAILED | Timeout |
| PRIMING | FirstSegmentReady | playlist && segment && atomic_publish | READY | R_NONE | Playable guarantee |
| PRIMING | PrimingTimeout | - | FAILED | R_PACKAGER_FAILED | Packager failed |
| READY | StopRequested | - | DRAINING | R_CLIENT_STOP | Begin graceful stop |
| DRAINING | DrainTimeout | - | STOPPING | R_NONE | Force teardown |
| DRAINING | StopComplete | - | STOPPED | R_NONE | Clean end |
| STOPPING | TeardownComplete | - | STOPPED | R_NONE | Final terminal state |
| ANY | WorkerError | map_error() | FAILED | R_* | Error mapping |
| ANY | ClientCancel | - | CANCELLED | R_CANCELLED | User-initiated abort |

---

## Error Mapping

- `map_error()` is the **single source of truth** for reason codes and HTTP
  status mapping
- No ad-hoc reason strings in handlers
- All failures MUST set a structured reason code from the `R_*` enum

---

## Client Behavior Contract (Normative)

This section defines **required** client behavior for `/api/v3` session lifecycle.
Clients MUST follow these rules to remain compatible across versions.

### Unknown States

- Clients **MUST** treat any unknown `state` value as "not ready yet" and
  continue polling
- Clients **MUST NOT** fail hard solely due to an unknown `state` value
- **Rationale**: Server may introduce additional intermediate states in future
  versions without breaking compatibility

### Admission Rejections (409 LEASE_BUSY)

`POST /api/v3/intents` **MAY** return `409 Conflict` with error code
`LEASE_BUSY` when capacity is exhausted.

- Clients **MUST** assume that **no session was created** when receiving 409
- Clients **SHOULD** respect `Retry-After` header (seconds) before retrying
- Clients **MUST NOT** attempt to `GET /api/v3/sessions/{id}` based on any
  locally fabricated or previous `sessionId` in response to 409

### READY Guarantee (Playable Contract)

When `GET /api/v3/sessions/{id}` returns `state=READY`, playback **MUST** be
immediately possible.

**READY implies**:

- The playlist (`index.m3u8`) exists and is served successfully, **AND**
- At least one media segment exists and is available, **AND**
- HLS artifacts have been atomically published (no partial playlist exposure)

**Client obligations**:

- Clients **SHOULD** begin playback immediately upon observing `READY`
- If a client observes `READY` but cannot fetch the playlist or first segment,
  clients **SHOULD** treat this as a server regression (surface actionable error
  and optionally retry), **not** as normal startup behavior

### Polling Guidance

- Clients **SHOULD** poll `GET /api/v3/sessions/{id}` until `READY` or a
  terminal state is reached
- Clients **MUST** treat `STARTING` and `PRIMING` as expected intermediate
  states
- Clients **MUST NOT** interpret `STARTING`/`PRIMING` as playback buffering
  conditions
- Buffering UX **MUST** be driven by media element events (e.g.,
  `waiting`/`stalled`) after playback begins, **not** by session state alone

### Terminal States (Polling Termination)

When a session enters a terminal state (`FAILED`, `CANCELLED`, or `STOPPED`):

- Clients **MUST** stop polling `GET /api/v3/sessions/{id}`
- Clients **MUST NOT** expect further state transitions
- Any subsequent client action requires creating a **new session** via
  `POST /api/v3/intents`

---

## Version History

- **v3.0.0** (2025-12-24): Initial FSM specification with normative client
  behavior contract
- **Unreleased**: Documentation improvements
