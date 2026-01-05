# Remux-First Decision Logic

## Architecture Decision: LIVE-DVR Always

**Policy:** There is no "Live without DVR" mode. Every live session is LIVE-DVR with EVENT playlist.

This means:
- Pause, rewind, and scrubber are always available
- CHASE mode (watching from recording start while recording) is not a special case
- Timeline is stable and segments remain until session cleanup

---

## Remux vs Transcode Decision

### Remux is preferred when source is compatible

**Criteria for remux (`-c copy`):**
1. ✅ Video codec: `h264` or `hevc`
2. ✅ Audio codec: `aac` or `mp3` (or no audio)
3. ✅ **NOT** interlaced (`field_order=progressive`)
4. ✅ Resolution ≤ `VideoMaxWidth` (if limit specified)

**Transcode required when:**
- ❌ Video: `mpeg2video`, `vc1`, or other non-h264/hevc codecs
- ❌ Audio: `ac3`, `dts`, `pcm` (Safari needs AAC)
- ❌ Interlaced source (requires `yadif` deinterlacing)
- ❌ Resolution exceeds limit (requires scaling)

---

## Implementation

### 1. Probe the source

```go
probe, err := ffmpeg.ProbeURL(ctx, streamURL)
if err != nil {
    // Fallback to transcode if probe fails
}
```

### 2. Check compatibility

```go
if probe.CanRemux(profile.VideoMaxWidth) {
    // Use remux: -c:v copy -c:a copy
} else {
    // Use transcode with appropriate filters
}
```

### 3. ffmpeg args

**Remux:**
```bash
ffmpeg -i <url> -c:v copy -c:a copy -f hls ...
```

**Transcode:**
```bash
ffmpeg -i <url> \
  -c:v libx264 -preset faster \
  -vf yadif=0:-1:1 \  # if interlaced
  -c:a aac -b:a 384k \
  -f hls ...
```

---

## Storage Layout

Both remux and transcode write to the **same directory**:

```
/recordings/<rec-id>/hls/
  index.m3u8           # EVENT playlist (grows)
  init.mp4             # fMP4 init segment
  seg_000001.m4s
  seg_000002.m4s
  ...
```

This directory serves:
- LIVE-DVR during session
- CHASE mode (start from beginning)
- VOD after session ends (playlist frozen or MP4 remux)

**No duplicate storage.** No separate paths for remux vs transcode.

---

## HLS Flags (Critical)

### LIVE-DVR (always)

```
-hls_flags append_list+omit_endlist+independent_segments+program_date_time+temp_file
-hls_playlist_type event
-hls_list_size 0  # Playlist grows (segments kept)
```

**NO `delete_segments`** – segments remain until session cleanup.

### VOD (finished recording)

```
-hls_flags independent_segments+temp_file
-hls_playlist_type vod
-hls_list_size 0
```

Playlist includes `#EXT-X-ENDLIST`.

---

## Safari Compatibility Requirements

### MIME Types (Critical for MediaError 4)

| File | Content-Type | Notes |
|------|-------------|-------|
| `index.m3u8` | `application/vnd.apple.mpegurl` | Playlist |
| `init.mp4` | `video/mp4` | fMP4 init |
| `seg_*.m4s` | `video/mp4` | **NOT** `video/iso.segment` |

**Reason:** Safari rejects `video/iso.segment` with MediaError code 4.

### Auth Consistency

All HLS resources (m3u8, init.mp4, segments) MUST use same auth:
- Same scope (`ScopeV3Read`)
- Same token validation
- No token gaps

### No gzip on video

Do NOT apply `Content-Encoding: gzip` to init.mp4 or .m4s files.

---

## Testing

### Unit tests

```bash
go test ./internal/v3/exec/ffmpeg/... -run TestStreamProbeResult_CanRemux
```

### Integration test checklist

```bash
# 1. Check MIME types
curl -I http://localhost:8080/api/v3/sessions/{id}/hls/index.m3u8
# Expected: Content-Type: application/vnd.apple.mpegurl

curl -I http://localhost:8080/api/v3/sessions/{id}/hls/init.mp4
# Expected: Content-Type: video/mp4

curl -I http://localhost:8080/api/v3/sessions/{id}/hls/seg_000001.m4s
# Expected: Content-Type: video/mp4

# 2. Verify playlist type
curl http://localhost:8080/api/v3/sessions/{id}/hls/index.m3u8
# Expected: #EXT-X-PLAYLIST-TYPE:EVENT
# Expected: #EXT-X-PROGRAM-DATE-TIME
# Expected: NO #EXT-X-ENDLIST (omit_endlist for live)

# 3. Verify segments remain (no delete)
# Start stream, wait 30s, check segment count grows
ls -1 /recordings/{id}/hls/seg_*.m4s | wc -l
# Count should increase over time, not stay fixed
```

---

## Migration Notes

### Changed behavior

| Before | After | Impact |
|--------|-------|--------|
| `delete_segments` in DVR | `delete_segments` removed | Segments persist → Timeline works |
| `.m4s` = `video/iso.segment` | `.m4s` = `video/mp4` | Safari MediaError 4 fixed |
| DVR window = fixed size | DVR window = unlimited grow | Full recording available |

### No breaking changes

- API unchanged
- Client compatibility maintained
- Storage path unchanged

---

## Next Steps

1. ✅ MIME types corrected
2. ✅ `delete_segments` removed from LIVE-DVR
3. ✅ ffprobe decision logic implemented
4. 🔄 ProfileSpec cleanup (VOD as state, not mode)

---

## References

- [args.go](../internal/v3/exec/ffmpeg/args.go) – ffmpeg argument construction
- [probe.go](../internal/v3/exec/ffmpeg/probe.go) – Stream analysis and remux decision
- [hls.go](../internal/v3/api/hls.go) – HLS delivery with correct MIME types
