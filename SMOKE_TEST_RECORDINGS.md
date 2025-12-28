# Recording Playback Smoke Test Guide

This document describes smoke test scenarios for the Recording Playback feature.

## Prerequisites

- Running xg2g instance with v3 worker enabled
- Enigma2 receiver with recordings
- Access to receiver's recording filesystem (NFS mount, bind mount, or local disk)

## Test Scenario 1: Finished Recording (Local Playback)

**Objective**: Verify local-first playback for stable, finished recordings.

### Setup

```bash
# docker-compose.yml or .env
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/recordings
XG2G_RECORDINGS_STABLE_WINDOW=2s
```

**Preconditions**:
- Recording exists on receiver at `/media/hdd/movie/recording.ts`
- Same file accessible locally at `/mnt/recordings/recording.ts`
- Recording is finished (file size stable)

### Test Steps

1. Request HLS playlist:
   ```bash
   curl -v http://localhost:8088/api/v3/recordings/{recordingId}/hls/playlist.m3u8
   ```

2. Check logs for source decision:
   ```bash
   docker logs xg2g 2>&1 | grep "recording playback source selected"
   ```

### Expected Results

**Log output**:
```json
{
  "level": "info",
  "recording_id": "...",
  "source_type": "local",
  "receiver_ref": "/media/hdd/movie/recording.ts",
  "source": "/mnt/recordings/recording.ts",
  "msg": "recording playback source selected"
}
```

**Behavior**:
- ✅ HTTP 302 redirect to HLS playlist
- ✅ `source_type=local` in logs
- ✅ V3 worker uses local file path (no HTTP stream from receiver)
- ✅ Playback starts successfully

## Test Scenario 2: Ongoing Recording (Fallback to Receiver)

**Objective**: Verify stability gate prevents streaming files being written.

### Setup

Same as Scenario 1.

**Preconditions**:
- Recording is actively being written (in-progress EPG timer)
- File size is changing during stability window

### Test Steps

1. Start a recording on the receiver
2. While recording is in progress, request HLS playlist:
   ```bash
   curl -v http://localhost:8088/api/v3/recordings/{recordingId}/hls/playlist.m3u8
   ```

3. Check logs with debug level:
   ```bash
   XG2G_LOG_LEVEL=debug docker logs xg2g 2>&1 | grep -A2 "unstable"
   ```

### Expected Results

**Debug log output**:
```json
{
  "level": "debug",
  "local_path": "/mnt/recordings/recording.ts",
  "stable_window": "2s",
  "msg": "file unstable, falling back to receiver"
}
```

**Info log output**:
```json
{
  "level": "info",
  "recording_id": "...",
  "source_type": "receiver",
  "receiver_ref": "/media/hdd/movie/recording.ts",
  "source": "http://10.10.55.64:8001/...",
  "msg": "recording playback source selected"
}
```

**Behavior**:
- ✅ `source_type=receiver` in logs
- ✅ Debug log indicates fallback reason (unstable)
- ✅ V3 worker uses Receiver HTTP stream
- ✅ Playback starts successfully (no corruption)

## Test Scenario 3: No Mapping Configured (Default Behavior)

**Objective**: Verify backward compatibility when feature is not configured.

### Setup

```bash
# No XG2G_RECORDINGS_MAP configured
# OR empty mappings
XG2G_RECORDINGS_MAP=
```

### Test Steps

1. Request HLS playlist for any recording
2. Check logs

### Expected Results

**Log output**:
```json
{
  "level": "info",
  "recording_id": "...",
  "source_type": "receiver",
  "receiver_ref": "/media/hdd/movie/recording.ts",
  "source": "http://10.10.55.64:8001/...",
  "msg": "recording playback source selected"
}
```

**Behavior**:
- ✅ `source_type=receiver` (always)
- ✅ No debug logs about unstable files
- ✅ Behavior identical to pre-feature version

## Test Scenario 4: File Not Found (Graceful Fallback)

**Objective**: Verify fallback when local file doesn't exist.

### Setup

```bash
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/recordings
```

**Preconditions**:
- Mapping configured
- Recording exists on receiver at `/media/hdd/movie/recording.ts`
- Local file does NOT exist at `/mnt/recordings/recording.ts`

### Test Steps

1. Request HLS playlist
2. Check logs

### Expected Results

**Log output**:
```json
{
  "level": "info",
  "recording_id": "...",
  "source_type": "receiver",
  "receiver_ref": "/media/hdd/movie/recording.ts",
  "source": "http://10.10.55.64:8001/...",
  "msg": "recording playback source selected"
}
```

**Behavior**:
- ✅ `source_type=receiver` (fallback)
- ✅ No errors in logs
- ✅ Playback works via Receiver HTTP

## Test Scenario 5: Multiple Paths (Longest Prefix)

**Objective**: Verify longest-prefix-first matching for overlapping paths.

### Setup

```bash
XG2G_RECORDINGS_MAP=/media/hdd/movie=/mnt/movies;/media/hdd/movie2=/mnt/movies2
```

**Preconditions**:
- Recording at `/media/hdd/movie/test.ts` → should map to `/mnt/movies/test.ts`
- Recording at `/media/hdd/movie2/test.ts` → should map to `/mnt/movies2/test.ts`

### Test Steps

1. Request HLS for recording from `/media/hdd/movie/test.ts`
2. Verify source is `/mnt/movies/test.ts`
3. Request HLS for recording from `/media/hdd/movie2/test.ts`
4. Verify source is `/mnt/movies2/test.ts`

### Expected Results

**Behavior**:
- ✅ `/media/hdd/movie2` matches before `/media/hdd/movie` (longest prefix)
- ✅ Both recordings map correctly to their respective local paths

## Verification Checklist

After running all scenarios:

- [ ] **Scenario 1**: Local playback works for stable files
- [ ] **Scenario 2**: Unstable files fall back to Receiver
- [ ] **Scenario 3**: No config = Receiver-only (backward compatible)
- [ ] **Scenario 4**: Missing local file falls back gracefully
- [ ] **Scenario 5**: Longest prefix matching works correctly

## Troubleshooting

If tests fail, check:

1. **Path mapping**: Ensure receiver path exactly matches (case-sensitive)
2. **File permissions**: xg2g must have read access to local files
3. **Mount point**: Verify NFS/bind mount is active and accessible
4. **Logs**: Enable `XG2G_LOG_LEVEL=debug` for detailed diagnostics

## Performance Validation (Optional)

For production deployments, measure:

1. **Playback start latency**: Time from request to first video frame
   - Local should be faster than Receiver HTTP
2. **Network bandwidth**: Local playback should use zero receiver bandwidth
3. **Stability window impact**: Measure delay introduced by stability check
   - Default 2s window adds ~2s to playback start for new recordings

## Regression Testing

Before each release, verify:

- No breaking changes to existing Receiver-only deployments
- No performance regressions for local playback
- No log spam at Info level for common fallback scenarios
