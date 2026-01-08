# End-to-End Smoke Test - Pre-Release Gate

**Status**: MANDATORY before release  
**Duration**: 5-10 minutes  
**Purpose**: Verify product is usable from user perspective

> [!CAUTION]
> This gate MUST pass before Docker/CI/Telemetry work. A green CI with broken auth/streaming is worthless.

## Test Matrix (4 Critical Flows)

### ✅ Flow 1: Cold Start

**Scenario**: Fresh container start, no previous state

**Steps**:

```bash
# 1. Stop any running instance
docker stop xg2g || true
docker rm xg2g || true

# 2. Clean state
rm -rf /tmp/xg2g-smoke-test
mkdir -p /tmp/xg2g-smoke-test/{data,config}

# 3. Minimal config
cat > /tmp/xg2g-smoke-test/config/config.yaml <<EOF
receiver:
  host: "YOUR_RECEIVER_IP"
  port: 80
  username: "root"
  password: "YOUR_PASSWORD"

api:
  listen: ":8088"
  
data_dir: "/data"
EOF

# 4. Start container
docker run -d \
  --name xg2g-smoke \
  -p 8088:8088 \
  -v /tmp/xg2g-smoke-test/data:/data \
  -v /tmp/xg2g-smoke-test/config/config.yaml:/etc/xg2g/config.yaml:ro \
  xg2g:3.1.3

# 5. Wait for startup
sleep 5
```

**Verification**:

```bash
# Health check
curl -f http://localhost:8088/healthz
# Expected: 200 OK

# Logs - no panic
docker logs xg2g-smoke 2>&1 | grep -i "panic\|fatal"
# Expected: empty output

# Process running
docker ps | grep xg2g-smoke
# Expected: container listed, status "Up"
```

**Pass Criteria**:

- [ ] Container starts without crash
- [ ] Health endpoint returns 200
- [ ] No panic/fatal in logs
- [ ] Process stays up (no restart loop)

---

### ✅ Flow 2: Auth/Security

**Scenario**: Verify authentication and LAN Guard behavior

**Test 2.1: No Auth (should fail)**

```bash
curl -i http://localhost:8088/api/v3/channels
# Expected: 401 Unauthorized (if auth enabled) OR 403 Forbidden (if LAN Guard blocks)
```

**Test 2.2: Valid Auth**

```bash
# If using API key
curl -i -H "Authorization: Bearer YOUR_API_KEY" http://localhost:8088/api/v3/channels
# Expected: 200 OK with channel list

# If using session
curl -i -c /tmp/cookies.txt -X POST http://localhost:8088/api/v3/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"YOUR_PASSWORD"}'
# Expected: 200 OK with session cookie

curl -i -b /tmp/cookies.txt http://localhost:8088/api/v3/channels
# Expected: 200 OK
```

**Test 2.3: LAN Guard (if enabled)**

```bash
# From non-LAN IP (simulate via X-Forwarded-For)
curl -i -H "X-Forwarded-For: 8.8.8.8" http://localhost:8088/api/v3/channels
# Expected: 403 Forbidden
```

**Pass Criteria**:

- [ ] Unauthorized access returns 401/403 (not 500)
- [ ] Valid credentials grant access (200)
- [ ] LAN Guard blocks external IPs correctly
- [ ] No middleware order issues (auth + LAN Guard work together)

---

### ✅ Flow 3: Live-TV Quick Check

**Scenario**: Start a live stream, verify no unnecessary transcoding

**Steps**:

```bash
# 1. Get channel list
CHANNEL_ID=$(curl -s http://localhost:8088/api/v3/channels | jq -r '.[0].id')

# 2. Start stream intent
INTENT=$(curl -s -X POST http://localhost:8088/api/v3/intents \
  -H "Content-Type: application/json" \
  -d "{\"channel_id\":\"$CHANNEL_ID\",\"profile\":\"copy\"}")

SESSION_ID=$(echo $INTENT | jq -r '.session_id')
PLAYLIST_URL=$(echo $INTENT | jq -r '.playlist_url')

# 3. Fetch playlist
curl -f "$PLAYLIST_URL"
# Expected: 200 OK, valid M3U8

# 4. Monitor CPU (should be low for copy profile)
docker stats xg2g-smoke --no-stream --format "{{.CPUPerc}}"
# Expected: <10% CPU (copy mode, no transcode)

# 5. Check logs for FFmpeg command
docker logs xg2g-smoke 2>&1 | grep -i "ffmpeg.*$SESSION_ID"
# Expected: -c copy in command (no transcode)
```

**Pass Criteria**:

- [ ] Stream starts (intent returns 200)
- [ ] Playlist accessible
- [ ] CPU usage low (<10% for copy mode)
- [ ] FFmpeg uses `-c copy` (no unnecessary transcode)
- [ ] No FFmpeg errors in logs

---

### ✅ Flow 4: VOD Minimal Flow

**Scenario**: Start VOD build, verify atomic publish or cleanup

**Test 4.1: VOD Build Success**

```bash
# 1. Start VOD build
RECORDING_ID="your_recording_id"
VOD_JOB=$(curl -s -X POST http://localhost:8088/api/v3/recordings/$RECORDING_ID/vod \
  -H "Content-Type: application/json" \
  -d '{"profile":"h264_720p"}')

JOB_ID=$(echo $VOD_JOB | jq -r '.job_id')

# 2. Monitor job status
while true; do
  STATUS=$(curl -s http://localhost:8088/api/v3/vod/jobs/$JOB_ID | jq -r '.state')
  echo "VOD Status: $STATUS"
  
  if [ "$STATUS" = "succeeded" ] || [ "$STATUS" = "failed" ]; then
    break
  fi
  
  sleep 5
done

# 3. On success, verify output exists
if [ "$STATUS" = "succeeded" ]; then
  OUTPUT_PATH=$(curl -s http://localhost:8088/api/v3/vod/jobs/$JOB_ID | jq -r '.output_path')
  ls -lh "$OUTPUT_PATH"
  # Expected: File exists, non-zero size
fi

# 4. No partial artifacts
ls -lh /tmp/xg2g-smoke-test/data/vod/
# Expected: Only completed builds, no .tmp or partial files
```

**Test 4.2: VOD Build Abort (Cleanup Test)**

```bash
# 1. Start VOD build
VOD_JOB=$(curl -s -X POST http://localhost:8088/api/v3/recordings/$RECORDING_ID/vod \
  -d '{"profile":"h264_720p"}')

JOB_ID=$(echo $VOD_JOB | jq -r '.job_id')

# 2. Wait 2 seconds, then cancel
sleep 2
curl -X DELETE http://localhost:8088/api/v3/vod/jobs/$JOB_ID

# 3. Verify cleanup
sleep 3
ls /tmp/xg2g-smoke-test/data/vod/workdir/ 2>/dev/null || echo "Workdir cleaned up"
# Expected: Workdir removed (cleanup successful)

# 4. Check status
STATUS=$(curl -s http://localhost:8088/api/v3/vod/jobs/$JOB_ID | jq -r '.state')
echo "Final status: $STATUS"
# Expected: "canceled" or "failed"
```

**Pass Criteria**:

- [ ] VOD build starts (job created)
- [ ] Success: Output file exists atomically
- [ ] Success: No partial/temp files
- [ ] Abort: Cleanup removes workdir
- [ ] Abort: No final output artifact

---

## Smoke Test Summary

**Before proceeding to Docker/CI/Telemetry, ALL flows must pass:**

| Flow | Status | Critical Issue |
|------|--------|----------------|
| Cold Start | ☐ | Container crash / panic |
| Auth/Security | ☐ | 500 errors / middleware order |
| Live-TV | ☐ | Stream fails / unnecessary transcode |
| VOD | ☐ | Partial artifacts / cleanup fails |

**Decision Rule**:

- All ✅ → Proceed to Docker hardening
- Any ❌ → Fix before release

---

## Common Failure Modes

### "Health check fails"

- **Cause**: Config invalid, dependency unreachable
- **Action**: Check logs for startup errors

### "Auth returns 500"

- **Cause**: Middleware misorder, auth handler panic
- **Action**: Verify middleware chain in `http.go`

### "Stream starts but high CPU on copy"

- **Cause**: Transcode triggered unnecessarily
- **Action**: Check policy decision logs

### "VOD leaves partial files"

- **Cause**: Cleanup not invoked, Phase-9 monitor bypassed
- **Action**: Verify `BuildMonitor.Run()` and `defer cleanup()`

---

## CI Integration (After Manual Smoke Pass)

Once manual smoke test passes, institutionalize:

```yaml
- name: E2E Smoke Test
  run: |
    # Cold start
    docker run -d --name smoke -p 8088:8088 xg2g:latest
    sleep 5
    curl -f http://localhost:8088/healthz
    
    # Auth (no 500)
    STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8088/api/v3/channels)
    [ "$STATUS" = "401" ] || [ "$STATUS" = "403" ] || exit 1
    
    # Cleanup
    docker stop smoke && docker rm smoke
```

---

**Next After Smoke Pass**: Docker verification, CI FFmpeg contract, Phase-10 telemetry
