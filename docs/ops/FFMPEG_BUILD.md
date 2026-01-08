# FFmpeg Build Automation - Production Readiness

**Status**: ✅ PRODUCTION READY  
**Version**: FFmpeg 7.1.3 (pinned)  
**Last Audit**: 2026-01-08

## Summary

xg2g uses a **pinned, reproducible FFmpeg build** (7.1.3) bundled into the container image. The build is:

- Deterministic (checksum-verified source)
- Scoped (no global LD_LIBRARY_PATH leak)
- Explicit (contract via ENV in Dockerfile)
- Auditable (verification commands below)

## Architecture

### Build Flow

```
Source → Checksum Verify → Configure → Build → Install → Wrapper → Container
(7.1.3)   (sha256)          (GPL+x264  (8 cores) (/opt/    (scoped  (runtime)
                             +x265                 ffmpeg)   LD_LIB)
                             +VAAPI
                             +HLS)
```

### Runtime Contract

- **Container**: FFmpeg accessed via `/usr/local/bin/ffmpeg` (wrapper script)
- **Wrapper**: Sets `LD_LIBRARY_PATH=/opt/ffmpeg/lib` (scoped to FFmpeg process only)
- **xg2g Config**: `ENV XG2G_FFMPEG_PATH="/usr/local/bin/ffmpeg"`

## Production Readiness Gates

### ✅ Gate 1: Path Consistency

**Requirement**: Build path must match runtime expectation

| Component | Path |
|-----------|------|
| Dockerfile build | `TARGET_DIR=/opt/ffmpeg` |
| Dockerfile runtime | `FFMPEG_HOME="/opt/ffmpeg"` |
| Wrapper default | `FFMPEG_HOME="${FFMPEG_HOME:-/opt/ffmpeg}"` |

**Status**: ALIGNED ✅

### ✅ Gate 2: No System FFmpeg Leakage

**Requirement**: Container must use pinned build, not distro packages

**Verification Commands** (run in container):

```bash
# Verify wrapper is used
docker run --rm <image> which ffmpeg
# Expected: /usr/local/bin/ffmpeg

# Verify pinned version
docker run --rm <image> ffmpeg -version | head -1
# Expected: ffmpeg version 7.1.3

# Verify library linkage
docker run --rm <image> ldd /opt/ffmpeg/bin/ffmpeg | grep libavcodec
# Expected: libavcodec.so.61 => /opt/ffmpeg/lib/libavcodec.so.61
```

**Status**: VERIFIED ✅

### ✅ Gate 3: Wrapper Robustness

**Requirement**: Wrappers must fail cleanly and deterministically

**Implementation Checklist**:

- [x] `set -euo pipefail` for strict error handling
- [x] Binary existence check (`[ ! -x "${FFMPEG_BIN}" ]`)
- [x] Clear error messages to stderr
- [x] Exit code 1 on failure
- [x] No LD_LIBRARY_PATH append (deterministic, no accumulation)

**Test** (wrapper with missing binary):

```bash
FFMPEG_HOME=/nonexistent ./scripts/ffmpeg-wrapper.sh -version
# Expected:
# ERROR: FFmpeg binary not found or not executable: /nonexistent/bin/ffmpeg
# Set FFMPEG_HOME or FFMPEG_BIN to the correct location
# (exit 1)
```

**Status**: ROBUST ✅

### ✅ Gate 4: xg2g Entry Point Contract

**Requirement**: Application must use wrapper, not raw FFmpeg

**Config Verification**:

- Source: `internal/config/runtime_env.go:202`
- Variable: `XG2G_FFMPEG_PATH` (environment-configurable)
- Dockerfile: `ENV XG2G_FFMPEG_PATH="/usr/local/bin/ffmpeg"`

**Status**: EXPLICIT CONTRACT ✅

## Verification Commands (Copy/Paste Reproducible)

Run these commands to verify production readiness:

```bash
# Local: Wrapper functionality
FFMPEG_HOME=/opt/xg2g/ffmpeg ./scripts/ffmpeg-wrapper.sh -version | head -1
# Expected: ffmpeg version 7.1.3

# Local: Error handling
FFMPEG_HOME=/nonexistent ./scripts/ffmpeg-wrapper.sh -version 2>&1 | head -2
# Expected: ERROR: FFmpeg binary not found...

# Container: After docker build
docker run --rm xg2g:latest which ffmpeg
# Expected: /usr/local/bin/ffmpeg

docker run --rm xg2g:latest ffmpeg -version | head -1
# Expected: ffmpeg version 7.1.3

docker run --rm xg2g:latest sh -c 'echo $XG2G_FFMPEG_PATH'
# Expected: /usr/local/bin/ffmpeg
```

## Build Configuration

### FFmpeg 7.1.3 Configure Flags

```bash
--prefix=/opt/ffmpeg
--enable-gpl
--enable-libx264
--enable-libx265
--enable-vaapi
--enable-protocol=hls,file,http,tcp
--enable-demuxer=mpegts,hls
--enable-muxer=hls,mpegts
--disable-debug
--disable-doc
--disable-static
--enable-shared
```

**Why these flags**:

- GPL: Required for x264/x265
- x264/x265: H.264/H.265 encoding
- VAAPI: Hardware acceleration (Intel/AMD)
- HLS protocols: Essential for streaming
- No debug/doc: Minimal build size
- Shared libs: Reusable across processes

## Local Development

### Build FFmpeg Locally

```bash
make setup  # Builds to /opt/xg2g/ffmpeg (or set TARGET_DIR)
```

### Use Wrappers (Recommended)

```bash
export XG2G_FFMPEG_PATH=$(pwd)/scripts/ffmpeg-wrapper.sh
export FFMPEG_HOME=/opt/xg2g/ffmpeg  # If built to custom location
```

### Manual PATH (Alternative)

```bash
export PATH=/opt/xg2g/ffmpeg/bin:$PATH
export LD_LIBRARY_PATH=/opt/xg2g/ffmpeg/lib
```

**Note**: Wrappers are preferred - they scope LD_LIBRARY_PATH and prevent global leakage.

## CI Recommendations

### Fast Assertion Job

```yaml
- name: Verify FFmpeg Build
  run: |
    docker run --rm xg2g:latest ffmpeg -version | grep -q "7.1.3"
    docker run --rm xg2g:latest sh -c '[ "$(which ffmpeg)" = "/usr/local/bin/ffmpeg" ]'
    docker run --rm xg2g:latest sh -c '[ "$XG2G_FFMPEG_PATH" = "/usr/local/bin/ffmpeg" ]'
```

**Why**:

- Prevents accidental version drift
- Ensures wrapper is installed correctly
- Validates environment contract

## Troubleshooting

### "FFmpeg binary not found"

- Check `FFMPEG_HOME` points to correct install prefix
- Verify `/opt/ffmpeg/bin/ffmpeg` exists
- Run wrapper with `bash -x` for debug output

### "error while loading shared libraries"

- Wrapper should handle this automatically
- If using raw binary, ensure `LD_LIBRARY_PATH=/opt/ffmpeg/lib`

### Version mismatch in container

- Rebuild image from scratch: `docker build --no-cache`
- Verify `TARGET_DIR` in Dockerfile matches runtime paths

## Maintenance

### Updating FFmpeg Version

1. Update `FFMPEG_VERSION` in `scripts/build-ffmpeg.sh`
2. Update checksum `EXPECTED_SHA256`
3. Test locally: `make setup`
4. Update this doc with new version
5. Rebuild container and run verification commands
6. Update SBOM and security scans

**Critical**: Always verify checksum before building new versions.
