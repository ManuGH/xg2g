# Client Playback Telemetry

`POST /api/v3/sessions/{sessionID}/telemetry` accepts a small, structured
snapshot from a client while its session is active. The request uses the
session's existing `v3:read` bearer scope and is subject to the global API rate
limit. The server limits the body to 8 KiB, rejects unknown or missing fields,
checks value ranges and timestamp drift, and does not accept reports for
terminal or expired sessions.

The accepted snapshot is published as a `session.client_telemetry` event on the
session event stream. Prometheus records aggregate distributions and error
deltas under bounded `platform`, `outcome`, and `kind` labels. Session IDs,
device identifiers, channel names, stream URLs, and log text are never metric
labels or accepted payload fields. Raw client snapshots are not persisted by
this endpoint; exact values are available to connected SSE observers, while
Prometheus retains only aggregates according to its configured retention.

The initial metric set is designed to separate three common client-side causes
of stutter:

- `xg2g_client_playback_ingest_gap_seconds` shows whether transport data stopped
  arriving even though the server-side stream remained available.
- `xg2g_client_playback_decoded_fps` and
  `xg2g_client_playback_error_delta_total` show decoder/continuity pressure in
  the same client reports.
- `xg2g_client_playback_audio_lead_seconds` and the reported audio underruns
  expose a shrinking audio cushion independently of video throughput.

Example PromQL for the client-reported 99th-percentile transport gap:

```promql
histogram_quantile(
  0.99,
  sum by (le, platform) (rate(xg2g_client_playback_ingest_gap_seconds_bucket[15m]))
)
```

To correlate a single playback precisely, connect to the session SSE stream and
collect `session.client_telemetry` alongside the existing server-side
`session.telemetry` events. The backend deliberately avoids a durable
per-session sample table: one-second client snapshots can grow quickly and
contain behavior data. If historical per-session playback investigation
becomes a product requirement, add a separately governed retention store with
sampling, expiry, and access controls rather than attaching session IDs to
Prometheus labels or writing every sample to the session lease database.

The backend endpoint and metrics do not start uploading telemetry by
themselves. A client must explicitly send the bounded snapshot; best-effort
upload failures should never block playback or session heartbeats.
