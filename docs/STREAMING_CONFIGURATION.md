# Enigma2 Streaming Configuration Guide

This document details the critical configuration required for stable streaming from Enigma2 receivers using `xg2g`.

## Core Principle: The `/web` Interface

`xg2g` relies on the receiver's OpenWebIf API to resolve the correct stream URL for a given service reference. This is critical because:

1. **Transcoding**: The receiver decides if and how to transcode the stream based on its capabilities and the request parameters.
2. **Port Mapping**: The correct streaming port (8001, 8002, 17999, etc.) is determined by the receiver's configuration.
3. **Authentication**: The `/web/stream.m3u` endpoint handles session tokens and anti-hijack tokens if configured.

### ⚠️ Critical Configuration Rule

> **DO NOT set `XG2G_STREAM_PORT` (Enigma2.StreamPort) individually unless you have a specific reason to bypass the receiver's logic.**

**Default Behavior (Recommended):**

- **Setting**: `Enigma2.StreamPort: 0` (in `config.yaml` or `registry.go` default)
- **Mechanism**: `xg2g` queries `/web/stream.m3u?ref=<ServiceRef>` -> Receiver returns `http://<ip>:<port>/<ServiceRef>` -> `xg2g` uses this URL.

**Direct Port (Not Recommended):**

- **Setting**: `Enigma2.StreamPort: 8001` (or 8002)
- **Mechanism**: `xg2g` constructs `http://<ip>:8001/<ServiceRef>` directly.
- **Risk**: Bypasses transcoding logic; may fail if receiver uses a different port (e.g., OSCam-emu relay on 17999) or requires specific auth tokens not present in a raw direct URL.

## Credentials & Authentication

Even if streaming ports (like 8001) are often unauthenticated, `xg2g` requires valid credentials to:

1. Query the `/web/stream.m3u` endpoint.
2. Inject basic auth into the resolved stream URL for FFmpeg (e.g., `http://user:pass@ip:port/...`) if the receiver requires it.

**Required Environment Variables:**
You must provide the login for your specific receiver. The system does not use hardcoded credentials.

```bash
export XG2G_E2_USER="root"              # Default for most OpenWebIf setups
export XG2G_E2_PASS="YOUR_PASSWORD"     # Your specific receiver password
```

## Troubleshooting Common Errors

### `R_PIPELINE_START_FAILED`

**Cause**: The streaming pipeline (FFmpeg) could not start.
**Fix**:

- Check logs for "exec: no command". Pass `XG2G_FFMPEG_BIN` or ensure `ffmpeg` is in `$PATH`.
- Check if `Enigma2.StreamPort` is 0 (correct) or if a forced port is unreachable.

### `R_PACKAGER_FAILED` w/ "No such file or directory"

**Cause**: FFmpeg tried to open the ServiceRef (`1:0:19...`) as a local file because it wasn't a valid URL.
**Fix**:

1. Ensure `Enigma2.StreamPort` is **0**.
2. Verify `XG2G_E2_HOST`, `USER`, and `PASS` are correct so `ResolveStreamURL` succeeds.
3. If using a direct port, ensure it's actually open on the receiver.

## Summary Checklist

- [ ] `XG2G_STREAM_PORT` is **UNSET** or `0`.
- [ ] `XG2G_E2_USER` and `XG2G_E2_PASS` are set.
- [ ] `XG2G_FFMPEG_BIN` points to a valid binary (or `ffmpeg` is in PATH).
