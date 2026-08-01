# Operational Guide: FFmpeg Preflight Troubleshooting

This document covers media-source FFmpeg preflight only.
For lifecycle/operator preflight covering startup, public deployment policy, and
shared install/upgrade/restore/rollback gates, use
`docs/ops/OPERATIONAL_LIFECYCLE_CONTRACT.md`.

This guide explains how to interpret and resolve issues related to the FFmpeg preflight check in `xg2g`.

## 1. Understanding Preflight

Preflight is a fast, deterministic gate performed before starting an FFmpeg transcoding process. It ensures the source stream is reachable and contains valid Transport Stream (TS) data.

### Preflight Logic

1. **Probe**: The system performs a small HTTP GET request capped by
   `enigma2.preflightTimeout` / `XG2G_E2_PREFLIGHT_TIMEOUT`.
2. **Sync Check**: It looks for 0x47 sync bytes at 188-byte intervals.
3. **Fallback**: If a Stream Relay source (port <relay-port>) fails, xg2g can fall
   back to port 8001 (standard Enigma2 stream). Default is **on** for receiver
   compatibility and can be disabled when operators want strict <relay-port> failure.

## 2a. Configuration

Configurable parameters to tune preflight behavior for different environments (e.g. slow relay startup, remote WAN).

| Config Key | Environment Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `enigma2.preflightTimeout` | `XG2G_E2_PREFLIGHT_TIMEOUT` | `10s` | Maximum duration to wait for initial TS sync byte. Increase for slow/encrypted channels (relay middleware). |

## 2. Preflight Reasons (Log Analysis)

When preflight fails, look for the `preflight_reason` field in the logs:

| Reason | Interpretation | Action |
| :--- | :--- | :--- |
| `http_status_503` | Tuner or stream relay unavailable (often due to tuner contention). | Check tuner availability on the receiver. |
| `http_status_401`/`403` | Authentication failure (wrong credentials or IP rejection). | Verify `enigma2.username`/`password` and receiver access. |
| `sync_miss` | Stream connected but didn't return valid TS data (e.g., "Service not found"). | Verify the Service Reference is correct. |
| `timeout` | Stream didn't return enough data within 1s. | Treat as receiver-side issue (relay/policy/whitelist/contended). |
| `short_read` | Connection closed before 3 sync packets were read. | Check for intermittent network issues. |

## 3. Decision Path: timeout + 0 bytes on <relay-port>

If `resolved_url` points to `:<relay-port>` and you see:

- `preflight_reason: timeout`
- `preflight_bytes: 0`

Then **xg2g is not the failing component**. The receiver did not deliver any TS
body within the configured preflight gate. With `fallbackTo8001=false`, the
correct behavior is to reject immediately instead of hiding the receiver-side
failure.

Receiver readiness gate checklist:

1. **Exclusive client**: ensure no VLC or other relay middleware client is connected.
2. **Whitelist / policy**: relay middleware can restrict by ServiceRef (`/etc/enigma2/whitelist_streamrelay`) and/or by client access (IP/ACL). Check both.
3. **Service readiness**: verify in `/web/tunerstat` that the tuned service is locked and delivering data.

If this gate fails, the root cause is receiver-side (relay middleware, policy, whitelist, tuner availability).

## 4. Fallback Configuration

Fallback is enabled by default for compatibility with receivers that expose both
Stream Relay (`<relay-port>`) and classic Enigma2 streaming (`8001`). Disable it when a
failed <relay-port> path should fail closed instead of trying the classic stream port.

| Config Key | Environment Variable | Default | Description |
| :--- | :--- | :--- | :--- |
| `enigma2.fallbackTo8001` | `XG2G_E2_FALLBACK_TO_8001` | `true` | Fallback to port 8001 if <relay-port> fails. |

> [!IMPORTANT]
> Fallback only triggers for sources originally resolved to port `<relay-port>`. It attempts to reach the same host on port `8001` with the same Service Reference.

## 5. Deep Dive: Optional Middleware & Port <relay-port>

When using `xg2g` with a receiver that has **optional middleware** enabled (e.g., for specific channel processing), the stream resolution works as follows:

1. **Resolution**: `xg2g` calls `/web/stream.m3u` on the receiver (port 80).
2. **Redirection**: OpenWebIF decides based on channel settings which port to use. For certain configurations, it may return a URL pointing to **port <relay-port>**.
3. **Delivery**: Optional middleware on the receiver processes the request on <relay-port> and delivers TS packets.

### Diagnostic Runbook

If you see `timeout` or `preflight_bytes: 0` for port <relay-port>, use these steps to verify:

#### Step 1: Verify URL Resolution

Check the `xg2g` logs for `resolved_url`. If it points to `:<relay-port>`, Stream Relay is active.

#### Step 2: Manual Data Probe

Run this command from the `xg2g` host:

```bash
# Replace <IP> and <SREF> with values from logs
curl -m 5 -v "http://<IP>:<relay-port>/<SREF>" | hexdump -C | head -n 5
```

- **Success**: You see data starting with `47` (TS Sync Byte).
- **Failure (Empty)**: Connection successful, but no data. Check middleware logs for processing errors.
- **Failure (Timeout)**: No connection. Check **Firewall**, **Whitelist/policy** (ServiceRef and/or client ACL), or if the process is running.

#### Step 3 (One-time): relay middleware Exclusivity Check (<relay-port>)

Some receivers/images expose port <relay-port> as an exclusive resource (single active client). If <relay-port> is exclusive, xg2g must enforce admission control (reject when busy) to prevent pile-ups.

Run from the xg2g host (ensure no VLC/other clients are connected):

```bash
sref="1:0:19:2B66:3F3:1:C00000:0:0:0:"
url="http://192.168.1.100:<relay-port>/$sref"

rm -f /tmp/relay_a.bin /tmp/relay_b.bin

# Stream A (background)
timeout 2 curl --no-buffer -s "$url" > /tmp/relay_a.bin &
sleep 0.2

# Stream B (foreground)
timeout 2 curl --no-buffer -s "$url" > /tmp/relay_b.bin
wait

wc -c /tmp/relay_a.bin /tmp/relay_b.bin
```

Interpretation:

- **both > 0 bytes**: <relay-port> is likely not exclusive.
- **one is 0 bytes or significantly smaller**: <relay-port> behaves as exclusive (admission control required).
- **both 0 bytes**: receiver/relay not delivering TS (readiness/policy issue).

### Common Root Causes for <relay-port> Issues

- **Configuration**: Channel may not be configured for middleware processing.
- **Readiness**: Middleware may need initialization time.
- **Latency > 500ms**: If `preflight_latency_ms` is consistently high, check receiver configuration.
- **Process Status**: Middleware process may not be running or misconfigured.

### Cleanup (Manual Tests)

If you have executed manual `curl` tests in the background, ensure they are terminated to free up tuner resources:

```bash
# Terminate all active curl connections to the receiver
pkill -9 curl

# Optional: Find specific PIDs for connections to the receiver
lsof -i @192.168.1.100
# Then kill the specific <PID>
```
