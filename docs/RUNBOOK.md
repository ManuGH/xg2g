# xg2g On-Call Runbook

This guide maps `xg2g_stream_start_total` failure reasons to concrete validation steps.

## Quick Triage Table

| Reason | Likely Cause | First Check (Logs) | First Check (Metric) |
|---|---|---|---|
| `stream_connect_reset` | Race condition: FFmpeg connected before receiver port was ready. | `Connection refused` in logs | `xg2g_stream_tune_duration_seconds` < 3s? |
| `zap_timeout` | Receiver WebAPI unresponsive or slow. | `context deadline exceeded` | `xg2g_stream_tune_duration_seconds` hitting cap? |
| `io_error` | Network instability or receiver reset connection mid-stream. | `Input/output error` | correlated with `reason="stream_connect_reset"`? |
| `ffmpeg_exit` | Invalid codec, profile mismatch, or corrupt input stream. | `Exit code: 1` | Check `ffmpeg_last_speed` for 0.0 |

---

## Triage Workflows

### 1. Reason: `stream_connect_reset`

**Symptom**: High rate of startup failures with "Connection refused/reset".
**Hypothesis**: The "Post-Zap Delay" is too short for the receiver to tune the channel.
**Validation**:

1. Check `xg2g_stream_tune_duration_seconds`: Is the median exactly 3.0s? If lower, the sleep might be bypassed.
2. Check Receiver Logs: Is `oscam-emu` crashing or restarting?
**Remediation**:

- Increase the sleep duration in `hls.go` (currently 3000ms).
- Verify receiver CPU load.

### 2. Reason: `zap_timeout`

**Symptom**: WebAPI calls failing after 20s (default timeout).
**Hypothesis**: Receiver is overloaded or stuck (Enigma2 spinner).
**Validation**:

- `curl -v "http://<RECEIVER_IP>/web/about"`
- Reboot receiver if unreachable.

### 3. Reason: `io_error`

**Symptom**: "Input/output error" during read.
**Hypothesis**: Network flake or receiver closed socket abruptly.
**Validation**:

- If happening *immediately* at start: Treat as `stream_connect_reset`.
- If happening *mid-stream*: Network connectivity issue between xg2g and receiver.

### 4. Scenario: High Delivery Gap (Serve Latency >> Disk Latency)

**Symptom**: `first_segment_serve_latency` is 5s+ higher than `first_segment_latency`.
**Hypothesis**: Reverse Proxy buffering, Client behavior, or Network latency.
**Validation & Fixes**:

**1. Check Reverse Proxy (Nginx/Traefik) Settings**:

- **Buffering**: Ensure `proxy_buffering off;` (Nginx) to stream chunks immediately.
- **Timeouts**: Verify `proxy_read_timeout` is sufficient.
- **TCP**: Enable `tcp_nodelay on;` for real-time delivery.
- **Sendfile**: `sendfile on;` generally helps, but check invalidation.
- **Gzip**: Ensure `gzip off;` for `.ts` files to prevent buffering for compression.

**2. Check Client/Network**:

- **Packet Loss**: High latency often implies retransmissions.
- **Client CPU**: Is the player decoding slow (requesting segments late)?
- **HLS Props**: Large `EXT-X-TARGETDURATION` forces clients to buffer longer before first play.
