# xg2g v3.0 FSM Tables (2026 Vision)

This is the **spec-first** baseline for the v3.0 re-architecture:

- **Zero-blocking ingress**: APIs never trigger receiver or FFmpeg work inline.
- **State Store as Source of Truth (SoT)**: All client-visible decisions come from a store lookup.
- **Workers are the only side-effect layer**: Receiver tuning, FFmpeg lifecycle, packaging.
- **Typed reason codes**: Observability and retry semantics are *data*, not string matching.

---

## Session FSM (client-visible)

### States

| State | Meaning |
|---|---|
| `NEW` | Ticket created, not yet scheduled to a worker |
| `STARTING` | Work has been accepted; pipeline being established |
| `READY` | Pipeline is serving (origin can serve manifests + segments) |
| `DRAINING` | Stop requested; allow short grace window (optional) |
| `STOPPING` | Worker tearing down resources |
| `FAILED` | Terminal failure (reason code set) |
| `CANCELLED` | Client/owner cancelled (reason code set) |
| `EXPIRED` | TTL elapsed (garbage collected) |

### Events → Guards → Actions → Next State

| Event | From | Guard | Action(s) | To | ReasonCode |
|---|---|---|---|---|---|
| `API_INTENT_ACCEPTED` | — | `serviceRef valid` | create SessionRecord; emit `session.start` | `STARTING` | `R_OK` |
| `WORKER_LEASE_ACQUIRED` | `STARTING` | `lease ok` | set `pipelineId`, `owner`, `correlationId` | `STARTING` | `R_OK` |
| `PIPELINE_READY` | `STARTING` | `pipelineId matches` | set `readyAt`, set `originBase` | `READY` | `R_OK` |
| `PIPELINE_FAILED` | `STARTING` | `pipelineId matches` | set `failedAt`, store `reason` | `FAILED` | `R_*` |
| `API_STOP` | `READY/STARTING` | `caller authorized` | emit `session.stop` | `DRAINING` | `R_OK` |
| `DRAIN_GRACE_EXPIRED` | `DRAINING` | — | emit `session.stop(force)` | `STOPPING` | `R_OK` |
| `WORKER_STOPPED` | `STOPPING/DRAINING` | — | clear worker fields; schedule GC | `EXPIRED` | `R_OK` |
| `API_CANCEL` | `NEW/STARTING` | `caller authorized` | emit `session.stop`; mark cancelled | `CANCELLED` | `R_CANCELLED` |
| `TTL_EXPIRED` | any non-terminal | — | mark expired; emit `session.stop(force)` | `EXPIRED` | `R_EXPIRED` |

**Notes**

- Session IDs are immutable. If a client wants a “retry”, it creates a **new session ID**.
- Idempotency is scoped to a short start window (e.g. 5 minutes), mapping `Idempotency-Key → sessionId`.

---

## Pipeline FSM (worker-internal)

### States

| State | Meaning |
|---|---|
| `INIT` | pipeline object created (no side effects yet) |
| `LEASED` | single-writer lease acquired for serviceKey |
| `TUNE_REQUESTED` | receiver zap/tune requested |
| `TUNE_VERIFYING` | polling/subscribe for readiness signals |
| `FFMPEG_STARTING` | ffmpeg process spawned |
| `PACKAGER_READY` | packager has produced initial manifest/segments |
| `SERVING` | steady state (manifest refresh + segment production) |
| `STOPPING` | tear down in progress |
| `FAIL` | terminal failure |

### Events → Guards → Actions → Next State

| Event | From | Guard | Action(s) | To | ReasonCode |
|---|---|---|---|---|---|
| `PIPELINE_CREATE` | — | — | create PipelineRecord | `INIT` | `R_OK` |
| `LEASE_OK` | `INIT` | lease acquired | start lease renew loop | `LEASED` | `R_OK` |
| `LEASE_DENIED` | `INIT` | — | requeue/backoff OR attach to existing | `FAIL` | `R_LIMIT_CONCURRENCY` |
| `TUNE_START` | `LEASED` | receiver available | call receiver zap | `TUNE_REQUESTED` | `R_OK` |
| `TUNE_SIGNAL_OK` | `TUNE_REQUESTED/TUNE_VERIFYING` | ref matches + PIDs + SNR | proceed | `FFMPEG_STARTING` | `R_OK` |
| `TUNE_TIMEOUT` | `TUNE_REQUESTED/TUNE_VERIFYING` | — | stop receiver attempt | `FAIL` | `R_TUNE_TIMEOUT` |
| `FFMPEG_SPAWNED` | `FFMPEG_STARTING` | pid ok | attach logs, health probe | `PACKAGER_READY` | `R_OK` |
| `FFMPEG_EXIT` | `FFMPEG_STARTING/PACKAGER_READY/SERVING` | — | capture exit code | `FAIL` | `R_FFMPEG_EXIT_<code>` |
| `PACKAGER_FIRST_MANIFEST` | `PACKAGER_READY` | manifest exists | mark origin ready | `SERVING` | `R_OK` |
| `STOP_REQUESTED` | any non-terminal | — | stop ffmpeg; release lease | `STOPPING` | `R_STOP_REQUESTED` |
| `STOPPED` | `STOPPING` | — | finalize, emit worker_stopped | `FAIL` or terminal cleanup | `R_OK` |

---

## Origin behaviour (critical for Safari burst patterns)

| Session State | Master/Variant Request | Response |
|---|---|---|
| `READY` | `*.m3u8` | serve from filesystem/cache |
| `STARTING` | `master.m3u8` | serve **minimal placeholder** (fast retry) |
| `STARTING` | `variant.m3u8` | `503` or placeholder (profile dependent) |
| `FAILED/CANCELLED/EXPIRED` | any | `404` with reason code header |

Minimal placeholder example (MVP):

```m3u8
#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-ENDLIST
```
