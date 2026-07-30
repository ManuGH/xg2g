# Phase 5.3 Runbook: Playback Lifecycle Soak

## Scope

`xg2g-soak` is an operator-run staging check for the current v3 playback
lifecycle:

1. resolve playback truth at `/api/v3/live/stream-info`;
2. start a session through `/api/v3/intents`;
3. wait for a playable session;
4. maintain its heartbeat while it is held; and
5. stop every accepted session.

The harness records lifecycle counts, cleanup failures, mean ready latency, the
random seed, and a pass/fail verdict in a private JSON report.

It does **not** prove GPU saturation, tuner preemption, CPU-pressure admission,
process/container chaos recovery, or hermetic library resolution. Those require
dedicated hardware-aware probes and must not be inferred from a green lifecycle
soak.

## Safety boundary

- Mutating profiles accept only loopback staging on port `8089`.
- Production port `8088` is rejected unconditionally.
- `--confirm-staging`, a private token file, and at least one real service
  reference are mandatory.
- API tokens are never accepted as command-line arguments. The token file must
  be a regular file with mode `0600` or stricter.
- The harness does not send signals to processes or stop containers.

These controls are defense in depth. Before a run, the operator remains
responsible for confirming that `127.0.0.1:8089` maps to the intended staging
instance and that the supplied channels may be exercised.

## Build and unit gate

From the repository root:

```bash
go test -race ./backend/cmd/xg2g-soak
mkdir -p ./artifacts/bin
go build -o ./artifacts/bin/xg2g-soak ./backend/cmd/xg2g-soak
```

The tests pin the current v3 request contract, mandatory cleanup, loopback/port
guards, token-file permissions, Prometheus fail-closed behavior, and the
duration/concurrency scheduler.

## Read-only smoke

Smoke checks `/readyz`. If `--prom-url` is provided, it also requires exactly
one selected xg2g `up` series with value `1`.

```bash
go run ./backend/cmd/xg2g-soak \
  --profile smoke \
  --base-url http://127.0.0.1:8089 \
  --prom-url http://127.0.0.1:9090 \
  --prom-selector '{job="xg2g",instance="xg2g-main"}' \
  --artifact-dir ./artifacts/soak/smoke
```

Omit the Prometheus flags when the monitoring overlay is not running. A smoke
run without `--prom-url` proves readiness only.

## Staging soak

Create a private token file without placing the token in shell history:

```bash
install -m 0600 /dev/null /tmp/xg2g-soak-token
${EDITOR:-vi} /tmp/xg2g-soak-token
```

Then run a bounded staging soak with real Enigma2 service references:

```bash
go run ./backend/cmd/xg2g-soak \
  --profile soak \
  --base-url http://127.0.0.1:8089 \
  --confirm-staging \
  --token-file /tmp/xg2g-soak-token \
  --service-ref '1:0:1:445D:453:1:C00000:0:0:0:' \
  --duration 1h \
  --cycles-per-second 0.1 \
  --max-inflight 2 \
  --hold 15s \
  --ready-timeout 45s \
  --artifact-dir ./artifacts/soak/staging
```

Repeat `--service-ref` to distribute cycles deterministically across multiple
channels. Set `--seed` to reproduce the same channel-selection sequence.

The `nightly` profile uses the same checks and defaults to an eight-hour
submission window:

```bash
go run ./backend/cmd/xg2g-soak \
  --profile nightly \
  --base-url http://127.0.0.1:8089 \
  --confirm-staging \
  --token-file /tmp/xg2g-soak-token \
  --service-ref '1:0:1:445D:453:1:C00000:0:0:0:' \
  --artifact-dir ./artifacts/soak/nightly
```

Nightly execution is operator-triggered until a dedicated staging/hardware
runner and secret delivery path are established. Repository CI must not point a
mutating soak at a shared or production instance.

## Verdict and artifacts

The command exits non-zero when readiness, the optional Prometheus target, any
playback lifecycle, or any session cleanup fails. It writes:

```text
<artifact-dir>/report.json
```

The report has mode `0600` and includes at most the first 100 detailed failures
while retaining complete aggregate counts. Preserve the seed and report with
the matching service logs for diagnosis. Reports may contain service
references and operational details and must not be committed.

## Closure criteria

Phase 5.3 is complete only when evidence from the current harness shows:

- [ ] three consecutive eight-hour staging runs pass;
- [ ] every accepted session is stopped successfully;
- [ ] no playback lifecycle fails;
- [ ] ready latency remains inside the agreed staging SLO; and
- [ ] reports and matching service telemetry are retained for each run.

GPU/tuner saturation, priority/preemption, CPU-pressure admission, chaos
recovery, and hermetic execution are separate future gates with their own
instrumentation and acceptance criteria.
