# End-to-End Smoke Test

**Duration**: 5 minutes  
**Purpose**: Verify product works from user perspective before release

---

## Quick Test Matrix

### 1. Cold Start

```bash
docker run -d --name xg2g-smoke -p 8088:8088 \
  -v /tmp/xg2g-test:/data \
  -e XG2G_E2_HOST="192.168.1.50" \
  xg2g:3.1.5

sleep 5
curl -f http://localhost:8088/healthz  # Expect: 200 OK
docker logs xg2g-smoke | grep -i "panic\|fatal"  # Expect: empty
```

**Pass:** Container starts, health check passes, no panics.

---

### 2. Auth/Security

```bash
# No auth → should fail
curl -i http://localhost:8088/api/v3/channels
# Expect: 401 Unauthorized or 403 Forbidden

# Valid auth → should work
curl -i -H "Authorization: Bearer YOUR_TOKEN" http://localhost:8088/api/v3/channels
# Expect: 200 OK with channel list
```

**Pass:** Unauthorized requests blocked, valid auth works.

---

### 3. Live Stream

```bash
# Start stream
CHANNEL_ID=$(curl -s http://localhost:8088/api/v3/channels | jq -r '.[0].id')
curl -s -X POST http://localhost:8088/api/v3/sessions \
  -H "Content-Type: application/json" \
  -d "{\"channel_id\":\"$CHANNEL_ID\"}" | jq .

# Check CPU (copy mode should be <10%)
docker stats xg2g-smoke --no-stream --format "{{.CPUPerc}}"
```

**Pass:** Stream starts, low CPU usage (no unnecessary transcoding).

---

### 4. VOD Build

```bash
# Start VOD build
curl -s -X POST http://localhost:8088/api/v3/recordings/REC_ID/vod | jq .

# Monitor job (check logs for completion)
docker logs -f xg2g-smoke

# Verify no partial files
ls /tmp/xg2g-test/vod/  # Should have complete files only, no .tmp
```

**Pass:** VOD builds complete, no partial artifacts.

---

## Summary

| Flow | Status | Critical Issue |
|------|--------|----------------|
| Cold Start | ☐ | Container crash / panic |
| Auth/Security | ☐ | 500 errors / bypass |
| Live-TV | ☐ | Stream fails / high CPU |
| VOD | ☐ | Partial files / cleanup |

**Rule**: All ✅ → Release ready. Any ❌ → Fix first.

---

## CI Integration

```yaml
- name: E2E Smoke
  run: |
    docker run -d --name smoke -p 8088:8088 xg2g:3.1.5
    sleep 5
    curl -f http://localhost:8088/healthz || exit 1
    docker stop smoke && docker rm smoke
```
