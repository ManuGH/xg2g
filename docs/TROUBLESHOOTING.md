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

The system implements a **Post-Zap Delay** to handle this race condition.

- **Mechanism**: A fixed `time.Sleep(3 * time.Second)` in `internal/proxy/hls.go` after successfully resolving the Web API stream.
- **Effect**: Gives the receiver time to stabilize the tuner and `oscam-emu` listener before FFmpeg attempts the connection.

### Important Notes

- **Do NOT force Port 8001**: Port 8001 is for unencrypted (FTA) streams. Encrypted streams *must* use the port returned by OpenWebIF (e.g., 17999), as this is where the decryption happens. Forcing 8001 will result in a connection but no valid video data (black screen or error).
- **Startup Latency**: It is normal for streams to take **13-15 seconds** to start (3s Zap delay + FFmpeg initialization + HLS buffering). This is expected behavior for encrypted Enigma2 streaming.

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
