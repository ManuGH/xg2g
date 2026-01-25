# Real-Time Drift Detection Walkthrough

## Goal

Verify that the `xg2g` daemon detects configuration and runtime drift in real-time and reports it via the `/status` API and CLI.

## Prerequisites

- Built `xg2g` daemon.
- Config file at default location (e.g. `/etc/xg2g/config.yaml` or defined via `$XG2G_CONFIG` / `--config`).
- `ffmpeg` installed (for runtime check).
- API Token with `v3:status` scope (and `v3:admin` for reload).

## Step 1: Start Daemon

Start the daemon in the background. It will perform an initial verification scan (60s cadence).

```bash
xg2g &
PID=$!
sleep 5 # Wait for startup
```

## Step 2: Verify Clean State

Check the status. It should be "healthy" with no drift.

```bash
xg2g status
# Output: ✅ System Healthy ...
```

API Response:

```bash
# Token requires v3:status scope
curl -s -H "Authorization: Bearer $XG2G_API_TOKEN" http://localhost:8088/api/v3/status | jq
```

Expected: `drift.detected` is `false` (field may be absent momentarily before the first verification run completes).

## Step 3: Simulate Config Drift

Modify the configuration file directly on disk (simulating an unauthorized edit).
Define your config path (ensure this matches the running daemon):

```bash
CFG=${XG2G_CONFIG:-}
if [ -z "$CFG" ]; then
  CFG="${XG2G_DATA:-/tmp}/config.yaml"
fi
echo "Using config: $CFG"
```

```bash
sed -i 's/logLevel: info/logLevel: debug/' "$CFG"
```

Wait for the next verification cycle (~60s). To speed up testing, we can restart the daemon (initial check runs on startup) OR wait.
For this walkthrough, we wait or trigger a reload (which might re-read, but drift is about mismatch between *running* and *file*).
Note: `ConfigChecker` compares *file content* vs *effective config in memory*.
By changing the file, the file hash changes. The memory hash (effective) remains "info" until reload.
So this IS drift: File says "debug", Memory says "info".

```bash
sleep 65
```

## Step 4: Verify Drift Detection

Check status again.

```bash
xg2g status
```

Expected Output:

```
⚠️ DRIFT DETECTED:
   - [config] config.fingerprint: expected '...', got '...'
```

API Response:

```json
{
  "status": "degraded",
  "drift": {
    "detected": true,
    "mismatches": [ ... ]
  }
}
```

## Step 5: Remediation (Reload)

Trigger a config reload via the Internal Admin API to sync memory with file.
Note: Requires `v3:admin` scope (or a token with sufficient privileges).

```bash
curl -X POST -H "Authorization: Bearer $XG2G_API_TOKEN" http://localhost:8088/internal/system/config/reload
```

OR restart the daemon.

After reload, memory becomes "debug". File is "debug". Hashes match. Drift resolved.

```bash
xg2g status
# Output: ✅ System Healthy ...
```

## Homelab Configuration (Optional)

In homelab environments, you might want to disable verification or change the interval to reduce I/O or noise.

### 1. Disable Verification

To turn off the background worker completely:

```bash
export XG2G_VERIFY_ENABLED=false
# Restart daemon
```

### 2. Change Interval (Cadence)

To run verification less frequently (e.g., every 10 minutes):

```bash
export XG2G_VERIFY_INTERVAL=10m
# Restart daemon
```

### 3. Expected Effects

* `/api/v3/status`: The drift block remains stable. If the daemon was previously running, it shows the last known state from `drift_state.json`. If verification is disabled, this file is not updated.
- **Logs**: `drift_detected` / `drift_resolved` events are edge-triggered. If the worker is stopped, no new logs will appear.
- **Metrics**: `xg2g_drift_detected` gauge remains at its last set value (or 0 if restarted with verification disabled).

### 4. Systemd Example

For persistent configuration in `/etc/default/xg2g` or a drop-in:

```ini
# /etc/systemd/system/xg2g.service.d/override.conf
[Service]
Environment="XG2G_VERIFY_ENABLED=true"
Environment="XG2G_VERIFY_INTERVAL=5m"
```
