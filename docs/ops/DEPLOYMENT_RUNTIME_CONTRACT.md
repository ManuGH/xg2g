<!-- GENERATED FILE - DO NOT EDIT. Source: templates/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md.tmpl -->
# GPU/CPU- **Verification**: Run `scripts/verify-runtime.sh` after build to ensure

  canonical contract compliance.

**Status**: CANONICAL - Single Source of Truth
**Last Updated**: 2026-01-08
**Applies To**: v3.1.7+

> [!IMPORTANT]
> This document defines **non-negotiable** behavior. No bauchgefühl, no interpretation.

## 1. Architectural Principles (Non-Negotiable)

### Separation of Concerns

- **Policy Layer** (`internal/control/vod/policy.go`): Decides Copy vs Transcode
- **Infra Layer** (`internal/infra/ffmpeg`): Decides CPU vs GPU execution
- **Control Layer**: MUST NOT contain hardware/exec details

**Violation = Architecture breach.**

## 2. GPU/CPU Selection - Canonical Behavior

### Live-TV Streaming

**Default**: Copy/Remux (no encode)

- CPU/GPU irrelevant (stream copy bypasses codec)
- Minimal latency, maximum compatibility

**Transcode Trigger** (exception cases only):

- Incompatible codec (e.g., HEVC → H.264 for Safari)
- Bit depth mismatch (10-bit → 8-bit)
- Safari DVR constraints (AAC audio requirement)

**GPU Usage in Live Path**:

- GPU allowed ONLY with guardrails:
  - Session timeout (30s max)
  - Process limits (max concurrent sessions)
  - Fallback to CPU on GPU failure
- **Fail-closed**: If GPU stalls/crashes → terminate session

### VOD Builds

**Default**: GPU preferred (throughput optimization)

**Safety Requirements** (Phase-9 VOD Monitor):

1. **Progress/Heartbeat**: Event-driven (not timestamp-based)
2. **Stall Detection**: Timeout → `Stop(grace=2s, kill=5s)` → Cleanup
3. **Atomic Publish**: `fs.Rename(OutputTemp, FinalPath)` on success only
4. **No Partial Outputs**: Cleanup on all failure modes

**GPU Failure Handling**:

- Start failure → `StateFailed` + `ReasonStartFail` + Cleanup
- Mid-build crash → `StateFailed` + `ReasonCrash` + Cleanup
- **Policy**: No build tools (curl, git, gcc) in final stage. Minimal
  packages for VAAPI/FFmpeg only.
- **Never** leave partial artifacts

## 3. Runtime Contract (Docker + Host)

### FFmpeg Wrapper Contract (MANDATORY)

**Production Behavior**:

```bash
/usr/local/bin/ffmpeg  → wrapper script
/usr/local/bin/ffprobe → wrapper script
↓
/opt/ffmpeg/bin/ffmpeg  (pinned 7.1.3)
/opt/ffmpeg/bin/ffprobe (pinned 7.1.3)
```

**Wrapper Guarantees**:

- Scoped `LD_LIBRARY_PATH=/opt/ffmpeg/lib` (no global leak)
- Error on missing binary (exit 1 with message)
- Deterministic (no state accumulation)

**xg2g Configuration**:

```dockerfile
ENV XG2G_FFMPEG_BIN="/usr/local/bin/ffmpeg"
ENV FFMPEG_HOME="/opt/ffmpeg"
```

**NO FALLBACK to system FFmpeg.**

```bash
# Verify user and groups
docker exec xg2g-prod id
```

Missing wrapper = hard fail.

### [P2] GPU Device Contract (VAAPI)

```bash
# Verify dri access
docker exec xg2g-prod ls -l /dev/dri/renderD128
```

:/dev/dri/renderD128

**Container Runtime**:

```yaml
devices:
  - /dev/dri/renderD128:/dev/dri/renderD128
```

**Detection Logic**:

- Device present → GPU available
- Device absent → GPU unavailable (fail-closed, use CPU)
- **Test**: `hwaccel=force` on host without device → MUST return 400

## 4. Homelab SOA Setup - Golden Path

### Docker Compose Reference

```yaml
version: '3.8'

services:
  xg2g:
    image: ghcr.io/manugh/xg2g:3.1.7
    container_name: xg2g
    restart: unless-stopped

    # Network
    network_mode: host  # or bridge with port mapping

    # GPU Access (Intel/AMD VAAPI)
    devices:
      - /dev/dri/renderD128:/dev/dri/renderD128

    # Volumes
    # Advanced: Digest-Pinning (High-Assurance Enforcement)
    # image: ghcr.io/manugh/xg2g@sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855
    volumes:
      - /var/lib/xg2g/recordings:/recordings
      - /var/lib/xg2g/tmp:/tmp/xg2g
      - /etc/xg2g/config.yaml:/etc/xg2g/config.yaml:ro

    # Environment
    environment:
      - XG2G_CONFIG=/etc/xg2g/config.yaml
      - XG2G_DATA=/var/lib/xg2g
      - **XG2G_HLS_ROOT**: Must point to `/var/lib/xg2g/hls` (canonical volume
  mapping target).
      # FFmpeg paths already set in image

    # Resources (optional, recommended)
    deploy:
      resources:
        limits:
          memory: 4G
        reservations:
          devices:
            - driver: nvidia  # if NVIDIA, use nvidia-docker
              count: 1
              capabilities: [gpu]
```

### Directory Structure

```
/var/lib/xg2g/
├── recordings/     # VOD outputs (persistent)
├── tmp/            # HLS segments (ephemeral)
└── sessions/       # Session state (ephemeral)

/etc/xg2g/
└── config.yaml     # Read-only config
```

### Permissions

- Container user: `xg2g:xg2g` (UID/GID 1000)
- `/dev/dri/renderD128`: Group `video` (container user must be member)

## 5. Verification - Mechanical Checks

### Check 1: FFmpeg Contract

**Command**:

```bash
docker run --rm xg2g:3.1.7 which ffmpeg
```

**Expected**: `/usr/local/bin/ffmpeg`

**Command**:

```bash
docker run --rm xg2g:3.1.7 ffmpeg -version | head -1
```

**Expected**: `ffmpeg version 7.1.3`

**Command**:

```bash
docker run --rm xg2g:3.1.7 sh -c 'echo $XG2G_FFMPEG_PATH'
```

**Expected**: `/usr/local/bin/ffmpeg`

**Failure Test**:

```bash
docker run --rm -e FFMPEG_HOME=/nonexistent xg2g:3.1.7 ffmpeg -version
```

**Expected**: `ERROR: FFmpeg binary not found or not executable: /nonexistent/bin/ffmpeg` (exit 1)

### Check 2: GPU Detection

**Command** (with GPU device):

```bash
docker run --rm --device /dev/dri/renderD128 xg2g:3.1.7 ls -l /dev/dri/
```

**Expected**: `renderD128` present

Command (hwaccel test):

```bash
docker run --rm --device /dev/dri/renderD128 xg2g:3.1.7 \
  ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -f lavfi -i testsrc -t 1 -f null -
```

Expected: Success (exit 0)

#### Failure

```bash
# Verify non-root user (UID 10001)
docker inspect --format='{{.Config.User}}' xg2g:3.1.7
```

Test (no device):

```bash
docker run --rm xg2g:3.1.7 \
  ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 -f lavfi -i testsrc -t 1 -f null -
```

**Expected**: Failure (exit non-zero, "Cannot open /dev/dri/renderD128")

### Check 3: VOD Safety (Phase-9 Gates)

**Command**:

```bash
go test ./internal/control/vod -run TestVOD_Cleanup -v -count=1
```

**Expected**: All `TestVOD_Cleanup_*` tests PASS

**Command**:

```bash
go test ./internal/control/vod -run TestVOD_AtomicPublish -v -count=1
```

**Expected**: All `TestVOD_AtomicPublish_*` tests PASS

**Assertions**:

- Stall → `Stop(grace=2s, kill=5s)` called once ✅
- Crash → Cleanup attempted ✅
- Success → `fs.Rename(OutputTemp, FinalPath)` ✅
- Failure → No final artifact ✅

## 6. CI Recommendations

### Fast Assertion Job (< 10s)

```yaml
- name: Verify Deployment Contract
  run: |
    # FFmpeg wrapper
    docker run --rm xg2g:3.1.7 sh -c '
      [ "$(which ffmpeg)" = "/usr/local/bin/ffmpeg" ] || exit 1
      ffmpeg -version | grep -q "7.1.3" || exit 1
      [ "$XG2G_FFMPEG_PATH" = "/usr/local/bin/ffmpeg" ] || exit 1
    '

    # VOD Safety (Gates M3/M4)
    go test ./internal/control/vod -run "TestVOD_Cleanup|TestVOD_AtomicPublish" -count=1
```

### GPU Force Behavior (Optional, needs GPU runner)

```yaml
- name: GPU Fail-Closed Test
  run: |
    # Without device, hwaccel=force MUST fail
    docker run --rm xg2g:3.1.7 \
      ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD128 \
      -f lavfi -i testsrc -t 1 -f null - 2>&1 | grep -q "Cannot open"
```

## 7. Troubleshooting

### "FFmpeg binary not found"

**Cause**: Wrapper cannot find `/opt/ffmpeg/bin/ffmpeg`
**Fix**: Verify `FFMPEG_HOME` is set correctly, rebuild image if needed

### "Cannot open /dev/dri/renderD128"

**Cause**: GPU device not mounted or permissions wrong
**Fix**:

- Add `--device /dev/dri/renderD128` to docker run
- Ensure container user in `video` group

### "Partial VOD output after crash"

- **Check**: Audit binary size and dynamic dependencies (`ldd`) during CI as
  detailed in Phase 10.
**Fix**: Verify `BuildMonitor.Run()` is invoked, check logs for cleanup assertions

### "System FFmpeg used instead of pinned"

**Cause**: Wrapper bypassed, `XG2G_FFMPEG_PATH` points to system binary
**Fix**: Set `XG2G_FFMPEG_PATH=/usr/local/bin/ffmpeg` explicitly

## 8. Golden Path Checklist

Before deploying to production, verify:

- [ ] FFmpeg wrapper contract verified (`which ffmpeg` → wrapper)
- [ ] Pinned version confirmed (`ffmpeg -version` → 7.1.3)
- [ ] GPU device mounted if GPU transcoding enabled
- [ ] VOD safety gates passing (M3, M4)
- [ ] Persistent volumes configured (`/var/lib/xg2g`)
- [ ] Config read-only (`/etc/xg2g/config.yaml:ro`)
- [ ] Resource limits set (memory, concurrent sessions)
- [ ] No system FFmpeg fallback (hard fail on missing wrapper)

## 9. Non-Negotiable Rules

1. **Control Layer**: No hardware/exec details
2. **FFmpeg**: Wrapper contract only, no system fallback
3. **GPU**: Fail-closed (stall/crash → terminate, cleanup)
4. **VOD**: Atomic publish (rename), no partial outputs
5. **Homelab**: Docker Compose golden path is reference

**Violation = breaking change requiring ADR.**

---

**References**:

- FFmpeg Build: `docs/ops/FFMPEG_BUILD.md`
- Phase-9 VOD Monitor: `phase9_walkthrough.md`
- Test Charta: `docs/ops/TEST_CHARTA.md`
