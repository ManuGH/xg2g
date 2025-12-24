# Troubleshooting Guide

## Stream Failures "Input/output error" on Startup

### Symptom

Streams fail to start immediately.

- **Log Message**: `[http @ ...] Will reconnect at 0 in 0 second(s), error=Input/output error.`
- **Client Behavior**: Stream initially fails or takes a long time to load.
- **Context**: Occurs primarily on encrypted channels using `oscam-emu`.

### Root Cause: Control Plane vs. Data Plane Race Condition

When `xg2g` requests a stream from OpenWebIF (Control Plane), it performs a "Zap" via the Web API.
For encrypted channels, the receiver must:

1. Tune the tuner (Satellite/Cable lock).
2. Initialize the Softcam (`oscam-emu`).
3. Open a TCP listener on the transcoding/streaming port (often **17999** for `oscam-emu`, distinct from the standard **8001**).

This process is asynchronous. `xg2g` receives the stream URL immediately after the Zap request and attempts to connect via FFmpeg. If FFmpeg connects before the receiver's TCP port is fully open and stable, the connection is refused or reset, leading to "Input/output error".

### Solution

The V3 streaming system implements a **Post-Zap Delay** plus a **Readiness Probe** to handle this race condition.

- **Mechanism**: The V3 orchestrator enforces a post-zap delay (configurable, default **5 seconds**) after successfully resolving the stream URL from OpenWebIF, then probes the resolved stream URL until it yields bytes before starting FFmpeg.
- **Effect**: Avoids "connect too early" flake by waiting for the receiver port to be actually readable, not just theoretically open.
- **Implementation**: See `internal/v3/exec/enigma2/client_ext.go` for stream resolution and tuning logic.

### If it still fails (fast-fail behavior)

If FFmpeg exits before the first HLS playlist/segments are ready, xg2g will stop waiting and fail the request instead of idling until a long timeout.

Typical log sequence:

- `ffmpeg process exited with error` (from the HLS profile)
- `HLS preflight failed` or `HLS streaming failed` with an error like `ffmpeg exited before ready`

This usually indicates the receiver never stabilized the returned stream URL (tuner busy, softcam not ready, port not open yet, receiver-side error). In this case, increasing “wait time” inside xg2g won’t help unless FFmpeg stays alive and reconnects successfully.

If you see an error like `stream not ready after zap`, xg2g could resolve the stream URL but never observed the stream returning bytes within the bounded readiness probe window.

### WebAPI timeouts

The WebAPI “zap” call uses a bounded HTTP timeout. If you see errors like `failed to call Web API: context deadline exceeded` / `zap_timeout`, check:

- OpenWebIF is reachable (host/port, credentials/token)
- receiver responsiveness under load (tuner/softcam state)
- LAN connectivity/packet loss

### Important Notes & Architecture Constraints

> [!IMPORTANT]
> **Understanding Port Redirection (8001 vs 17999)**
>
> 1. **Entry Point**: xg2g talks to the **OpenWebIF API** (Port 80) to request a stream (`/web/stream.m3u`).
> 2. **Redirect**: OpenWebIF returns a playlist containing the **actual** stream URL.
>    - For **FTA (Free-to-Air)** channels, this is usually Port **8001** (direct TS stream).
>    - For **Encrypted** channels, this is usually Port **17999** (served by `oscam-emu` or configured streamer which handles decryption).
> 3. **The Trap**: Developers often try to "fix" connection issues by hardcoding Port 8001. **This is wrong.** If you force Port 8001 for an encrypted channel, you will get a connection, but the stream will be black/invalid because it bypasses the decryption layer. You *must* follow the port returned by OpenWebIF.
>
> **The Race Condition (Why the Delay Exists)**
>
> When OpenWebIF returns the URL for Port 17999, it does *not* guarantee that `oscam-emu` has finished initializing the listener on that port. The receiver is still:
>
> 1. Locking the tuner.
> 2. Initializing the CAM.
> 3. Opening the socket.
>
> If xg2g (FFmpeg) connects instantly (ms after receiving the URL), it hits `Connection Refused`. The **5-second Post-Zap Delay** is mandatory to allow this sequence to complete.

---

## Known Limitations & Deferred Work

### DVR: Recording Conflict Detection Not Implemented

**Status**: Intentionally deferred (not blocking production use)

**Context**: The DVR keyword-based recording engine (`internal/dvr/engine.go`) currently does not detect scheduling conflicts when multiple recordings overlap the same tuner timeslot.

**Impact**:

- If multiple keywords match events that air simultaneously, all will be scheduled without conflict warnings
- The actual recording behavior depends on the receiver's tuner availability
- Users with multi-tuner receivers may not experience issues

**Workaround**:

- Review the DVR schedule via the API before critical recordings
- Use conservative keyword rules to minimize overlap risk
- Consider manual scheduling for high-priority recordings

**Tracking**: See `internal/dvr/engine.go:296` TODO comment. Implementation requires DetectConflicts logic that accounts for tuner count and existing timer state.

**Future Work**: Add conflict detection with configurable policies (warn, skip, or use tuner priority).
