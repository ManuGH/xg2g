<!-- GENERATED FILE - DO NOT EDIT. Source: backend/templates/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md.tmpl -->
# GPU/CPU Runtime Contract

**Verification**: Run `backend/scripts/verify-runtime.sh` after build to ensure canonical contract compliance.

**Status**: CANONICAL - Single Source of Truth
**Last Updated**: 2026-03-26
**Applies To**: v3.4.3+

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
/opt/ffmpeg/bin/ffmpeg  (pinned 8.1)
/opt/ffmpeg/bin/ffprobe (pinned 8.1)
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

### [P2] GPU Device Contracts (VAAPI / NVENC)

```bash
# /dev/dri hosts
docker exec xg2g-prod sh -lc 'ls -1 /dev/dri/renderD* 2>/dev/null || true'

# NVIDIA hosts
docker exec xg2g-prod sh -lc 'ls -1 /dev/nvidia* 2>/dev/null || true'
```

**Container Runtime**:

```yaml
services:
  xg2g: {}
```

Base production compose stays device-neutral.
Hardware-enabled hosts opt in through repo-managed overlays:

- `docker-compose.gpu.yml` for `/dev/dri` / VAAPI / QuickSync-class Linux hosts
- `docker-compose.nvidia.yml` for NVIDIA runtime / NVENC hosts

`compose-xg2g.sh` auto-loads whichever optional overlays are present, after the
base compose. Operators may also set `COMPOSE_FILE` in `/etc/xg2g/xg2g.env`
for explicit file selection. For `/dev/dri` hosts, the checked-in GPU overlay
is a marker: the helper expands it into render-node-only device entries for
every visible `/dev/dri/renderD*` path at runtime.

**Runtime Config Requirement**:

For containerized deployments, mounting `/dev/dri` is now sufficient by
default. When `ffmpeg.vaapiDevice` and `XG2G_VAAPI_DEVICE` are both unset,
xg2g auto-detects the first visible `/dev/dri/renderD*` node at startup and
uses it for the normal VAAPI preflight.

On Linux, that same `/dev/dri` path is the intended operator contract for
Intel iGPU / QuickSync-class deployments as well. xg2g currently standardizes
those hosts onto the VAAPI-backed path instead of maintaining a separate QSV
runtime branch.

NVIDIA / NVENC remains a different container contract: it depends on the
NVIDIA Container Toolkit / GPU runtime and device reservation or injection, not
on `/dev/dri` alone. The repo now ships that path as
`docker-compose.nvidia.yml`. On those hosts, xg2g runs a startup NVENC
preflight and automatically routes hardware-backed profiles onto verified
`*_nvenc` encoders when the FFmpeg build and the visible NVIDIA runtime support
them.

Operators can still pin or override the render node explicitly through one of:

```yaml
ffmpeg:
  vaapiDevice: /dev/dri/renderD128
```

or:

```bash
XG2G_VAAPI_DEVICE=/dev/dri/renderD128
```

In Docker/Compose, an explicitly empty `XG2G_VAAPI_DEVICE=` disables that
auto-detect path and forces CPU fallback even if `/dev/dri/renderD*` is
mounted. That gives operators a deterministic CPU-only escape hatch without
removing the `/dev/dri` overlay.

**Detection Logic**:

- GPU override configured + device present + preflight passed → GPU available
- GPU override unset + `/dev/dri/renderD*` visible + preflight passed → GPU available
- NVIDIA runtime visible + NVENC preflight passed → GPU available
- GPU override explicitly empty, device absent, or preflight failed → GPU unavailable (fail-closed, use CPU)
- **Test**: `hwaccel=force` without override/device → MUST return 400

## 4. Homelab SOA Setup - Golden Path

### Docker Compose Reference

```yaml
version: '3.8'

services:
  xg2g:
    image: ghcr.io/manugh/xg2g:v3.4.3
    container_name: xg2g
    restart: unless-stopped

    # Network
    network_mode: host  # or bridge with port mapping

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
      # Optional override. Default resolves to "$XG2G_DATA/hls" when unset.
      # - XG2G_HLS_ROOT=/var/lib/xg2g/hls
      # FFmpeg paths already set in image

    # Resources (optional, recommended)
    deploy:
      resources:
        limits:
          memory: 4G
```

**Optional `/dev/dri` Overlay (`docker-compose.gpu.yml`)**:

```yaml
services:
  xg2g: {}
```

Use the overlay only on hosts that actually expose one or more render nodes and
invoke it through `compose-xg2g.sh`. The helper turns this marker into
render-node-only device entries at runtime. The base compose must stay valid on
CPU-only systems so runtime CPU fallback remains reachable.

**Optional NVIDIA Overlay (`docker-compose.nvidia.yml`)**:

```yaml
services:
  xg2g:
    deploy:
      resources:
        reservations:
          devices:
            - driver: nvidia
              count: 1
              capabilities: [gpu]
```

Use this overlay only on hosts with the NVIDIA Container Toolkit / CDI
runtime configured. For multi-GPU hosts, operators may replace `count: 1`
with explicit `device_ids`.

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

- Container user: `xg2g:xg2g` (UID/GID 10001)
- `/dev/dri/renderD*`: Group `video` (container user must be member)
- NVIDIA hosts: runtime injection must expose `/dev/nvidia*` into the container; no separate `/dev/dri` mount is required

## 5. Verification - Mechanical Checks

### Check 1: FFmpeg Contract

**Command**:

```bash
docker run --rm xg2g:v3.4.3 which ffmpeg
```

**Expected**: `/usr/local/bin/ffmpeg`

**Command**:

```bash
docker run --rm xg2g:v3.4.3 ffmpeg -version | head -1
```

**Expected**: `ffmpeg version 8.1`

**Command**:

```bash
docker run --rm xg2g:v3.4.3 sh -c 'echo $XG2G_FFMPEG_BIN'
```

**Expected**: `/usr/local/bin/ffmpeg`

**Failure Test**:

```bash
docker run --rm -e FFMPEG_HOME=/nonexistent xg2g:v3.4.3 ffmpeg -version
```

**Expected**: `ERROR: FFmpeg binary not found or not executable: /nonexistent/bin/ffmpeg` (exit 1)

### Check 2A: `/dev/dri` / VAAPI Detection

**Command** (with `/dev/dri` contract):

```bash
docker run --rm --device /dev/dri:/dev/dri xg2g:v3.4.3 \
  sh -lc 'ls -1 /dev/dri/renderD*'
```

**Expected**: at least one `renderD*` path is printed

Live-runtime note: this check alone is insufficient. It only proves the device
tree is mounted, not that xg2g will choose GPU for session startup.

Command (hwaccel test):

```bash
docker run --rm --device /dev/dri:/dev/dri xg2g:v3.4.3 \
  sh -lc 'node="$(ls /dev/dri/renderD* | head -n1)"; test -n "$node"; ffmpeg -hwaccel vaapi -hwaccel_device "$node" -f lavfi -i testsrc -t 1 -f null -'
```

Expected: Success (exit 0)

Live-runtime requirement: also verify that the runtime config resolves to the
same device. Manual FFmpeg success alone does not prove the live playback path
will choose GPU, but a visible `/dev/dri/renderD*` no longer needs an extra
container env override on the 2026 image line.

#### Failure

```bash
# Verify non-root user (UID 10001)
docker inspect --format='{{.Config.User}}' xg2g:v3.4.3
```

Test (no device):

```bash
docker run --rm xg2g:v3.4.3 \
  ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD999 -f lavfi -i testsrc -t 1 -f null -
```

**Expected**: Failure (exit non-zero, "Cannot open /dev/dri/renderD...")

### Check 2B: NVIDIA / NVENC Detection

**Command** (with NVIDIA runtime):

```bash
docker run --rm --gpus all xg2g:v3.4.3 \
  sh -lc 'ls -1 /dev/nvidia* 2>/dev/null'
```

**Expected**: `/dev/nvidiactl` and at least one `/dev/nvidiaN` path are printed

Command (NVENC encode test):

```bash
docker run --rm --gpus all xg2g:v3.4.3 \
  ffmpeg -f lavfi -i testsrc=duration=0.2:size=1280x720:rate=25 -c:v h264_nvenc -frames:v 5 -f null -
```

Expected: Success (exit 0)

Failure (runtime absent):

```bash
docker run --rm xg2g:v3.4.3 \
  ffmpeg -f lavfi -i testsrc=duration=0.2:size=1280x720:rate=25 -c:v h264_nvenc -frames:v 5 -f null -
```

**Expected**: Failure (exit non-zero, "no capable devices found", "OpenEncodeSessionEx failed", or a similar NVENC runtime error)

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
    docker run --rm xg2g:v3.4.3 sh -c '
      [ "$(which ffmpeg)" = "/usr/local/bin/ffmpeg" ] || exit 1
      ffmpeg -version | grep -q "8.1" || exit 1
      [ "$XG2G_FFMPEG_BIN" = "/usr/local/bin/ffmpeg" ] || exit 1
    '

    # VOD Safety (Gates M3/M4)
    go test ./internal/control/vod -run "TestVOD_Cleanup|TestVOD_AtomicPublish" -count=1
```

### GPU Force Behavior (Optional, needs GPU runner)

```yaml
- name: VAAPI Fail-Closed Test
  run: |
    # Without device, hwaccel=force MUST fail
    docker run --rm xg2g:v3.4.3 \
      ffmpeg -hwaccel vaapi -hwaccel_device /dev/dri/renderD999 \
      -f lavfi -i testsrc -t 1 -f null - 2>&1 | grep -q "Cannot open"

- name: NVENC Runtime Smoke
  run: |
    docker run --rm --gpus all xg2g:v3.4.3 \
      ffmpeg -f lavfi -i testsrc=duration=0.2:size=1280x720:rate=25 -c:v h264_nvenc -frames:v 5 -f null -
```

## 7. Troubleshooting

### "FFmpeg binary not found"

**Cause**: Wrapper cannot find `/opt/ffmpeg/bin/ffmpeg`
**Fix**: Verify `FFMPEG_HOME` is set correctly, rebuild image if needed

### "Cannot open /dev/dri/renderD..."

**Cause**: GPU device not mounted or permissions wrong
**Fix**:

- Add the `/dev/dri` overlay or `--device /dev/dri:/dev/dri` to `docker run`
- Ensure container user in `video` group
- If the host exposes multiple render nodes, pin the intended one through `XG2G_VAAPI_DEVICE`

### "OpenEncodeSessionEx failed" / "no capable devices found"

**Cause**: NVIDIA runtime not injected, driver/toolkit mismatch, or GPU lacks the required NVENC feature set
**Fix**:

- Add the NVIDIA overlay or run with `--gpus all`
- Verify the host runtime with `nvidia-smi`
- Verify the image still reports `h264_nvenc` / `hevc_nvenc` in `ffmpeg -encoders`

### "Partial VOD output after crash"

- **Check**: Audit binary size and dynamic dependencies (`ldd`) during CI as
  detailed in Phase 10.
**Fix**: Verify `BuildMonitor.Run()` is invoked, check logs for cleanup assertions

### "System FFmpeg used instead of pinned"

**Cause**: Wrapper bypassed; `ffmpeg` resolves to a system binary instead of the pinned wrapper
**Fix**: Set `XG2G_FFMPEG_BIN=/usr/local/bin/ffmpeg` explicitly

## 8. Golden Path Checklist

Before deploying to production, verify:

- [ ] FFmpeg wrapper contract verified (`which ffmpeg` → wrapper)
- [ ] Pinned version confirmed (`ffmpeg -version` → 8.1)
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
