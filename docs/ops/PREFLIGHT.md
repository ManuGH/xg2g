# Operational Guide: FFmpeg Preflight Troubleshooting

This guide explains how to interpret and resolve issues related to the FFmpeg preflight check in `xg2g`.

## 1. Understanding Preflight

Preflight is a fast, recursive check performed before starting an FFmpeg transcoding process. It ensures the source stream is reachable and contains valid Transport Stream (TS) data.

### Preflight Logic

1. **Probe**: The system performs a small HTTP GET request (capped at 1s).
2. **Sync Check**: It looks for 0x47 sync bytes at 188-byte intervals.
3. **Fallback**: If a Stream Relay source (port 17999) fails, it can optionally fall back to port 8001 (standard Enigma2 stream).

## 2. Preflight Reasons (Log Analysis)

When preflight fails, look for the `preflight_reason` field in the logs:

| Reason | Interpretation | Action |
| :--- | :--- | :--- |
| `http_status_503` | Tuner or stream relay unavailable (often due to tuner contention). | Check tuner availability on the receiver. |
| `http_status_401`/`403` | Authentication failure (wrong credentials or IP rejection). | Verify `enigma2.username`/`password` and receiver access. |
| `sync_miss` | Stream connected but didn't return valid TS data (e.g., "Service not found"). | Verify the Service Reference is correct. |
| `timeout` | Stream didn't return enough data within 1s. | Check network latency or receiver CPU load. |
| `short_read` | Connection closed before 3 sync packets were read. | Check for intermittent network issues. |

## 3. Fallback Configuration

If you experience frequent `sync_miss` or `timeout` errors with encrypted channels (Stream Relay), enable the fallback:

```yaml
# config.yaml
enigma2:
  fallbackTo8001: true
```

> [!IMPORTANT]
> Fallback only triggers for sources originally resolved to port `17999`. It attempts to reach the same host on port `8001` with the same Service Reference.
