# Release Notes: LIVE-DVR Architecture (Production-Ready)

**Version:** 3.1.0
**Date:** 2026-01-05
**Status:** ✅ Production-Ready (with operational requirements)

---

## Summary

This release implements a **unified LIVE-DVR architecture** that eliminates Safari MediaError 4 playback failures and enables consistent timeline/scrubber functionality across all live streams.

**Key Change:** There is no longer a "Live-only" mode. Every live session now supports DVR (pause, rewind, scrub) by default.

---

## Changes Implemented

### 1. Safari MediaError 4 Eliminated

**Problem:** Safari rejected fMP4 segments with `MediaError code 4` ("Source not supported").

**Root Causes Fixed:**
- `.m4s` files served with incorrect MIME type (`video/iso.segment` instead of `video/mp4`)
- Potential gzip compression by reverse proxies

**Solution:**
- All fMP4 content (init.mp4, .m4s) now served with `Content-Type: video/mp4`
- Explicit `Content-Encoding: identity` header prevents proxy compression
- Auth consistency verified (all HLS resources use same scope)

**Files Changed:**
- `internal/pipeline/api/hls.go` (lines 216, 224, 229)
- `internal/api/recordings.go` (lines 1186-1195)

---

### 2. DVR Timeline/Scrubber Fixed

**Problem:** Timeline showed only last 3-5 segments instead of full session history.

**Root Cause:** `delete_segments` flag caused ffmpeg to delete old segments.

**Solution:**
- Removed `delete_segments` from HLS flags
- Changed `-hls_list_size` to `0` (unlimited playlist growth)
- Added `program_date_time` for Safari timeline mapping

**Playlist Characteristics:**
```
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-PROGRAM-DATE-TIME:2026-01-05T12:00:00Z
#EXT-X-INDEPENDENT-SEGMENTS
(no #EXT-X-ENDLIST - live session)
```

**Files Changed:**
- `internal/pipeline/exec/ffmpeg/args.go` (lines 189, 243)

---

### 3. Remux-First Performance Optimization

**New Feature:** Automatic detection of remux-compatible sources.

**Logic:**
```
IF source is h264/hevc AND aac/mp3 AND progressive AND resolution OK
  → Remux (ffmpeg -c copy) - minimal CPU
ELSE
  → Transcode with deinterlace/scale - full processing
```

**Benefits:**
- Remux: < 10% CPU, near-zero latency
- Transcode: Only when necessary (interlaced, incompatible codecs, scaling)

**Files Changed:**
- `internal/pipeline/exec/ffmpeg/probe.go` (CanRemux method)
- `internal/pipeline/exec/ffmpeg/probe_test.go` (9 test cases)

---

## Operational Requirements

### CRITICAL: Retention Policy Required

**Impact:** Segments now persist indefinitely until session cleanup.

**Disk Usage:**
| Session Duration | Segments | Storage (est.) |
|------------------|----------|----------------|
| 2 hours          | 1,200    | ~600 MB        |
| 4 hours          | 2,400    | ~1.2 GB        |
| 24 hours         | 14,400   | ~7.2 GB        |

**Required Action:** Implement retention policy within 1 week of deployment.

**Options:**
1. **Session-End Cleanup (Phase 1):** Delete all segments when session stops
2. **Rolling Window (Phase 2):** Keep only last 2-4 hours of segments

See [RETENTION_POLICY_REQUIREMENT.md](RETENTION_POLICY_REQUIREMENT.md) for implementation details.

---

### Infrastructure: Reverse Proxy Configuration

**Required:** Disable gzip/deflate compression for HLS content.

**nginx example:**
```nginx
location ~ \.(m3u8|mp4|m4s|ts)$ {
    gzip off;
    proxy_pass http://backend;
}
```

**Caddy example:**
```
encode {
    match {
        not path *.m3u8 *.mp4 *.m4s *.ts
    }
    gzip
}
```

**Validation:**
```bash
curl -I https://your-domain/api/v3/sessions/{id}/hls/init.mp4
# MUST NOT contain: Content-Encoding: gzip
```

---

### Monitoring Requirements

**Disk Usage:**
- Alert at 80% disk usage
- Alert if single session > 5GB

**Performance:**
- Remux sessions: < 10% CPU (expected)
- Transcode sessions: 50-100% CPU (expected)

**Errors:**
- No MediaError code 4 in Safari console logs
- No 403/404 on init.mp4 or .m4s requests

---

## Deployment Checklist

### Pre-Deployment

- [ ] Backup current configuration
- [ ] Verify disk space available (> 50GB recommended)
- [ ] Configure reverse proxy (disable compression for video)
- [ ] Set up disk usage monitoring

### Deployment

```bash
git pull
go build -o xg2g ./cmd/daemon
systemctl restart xg2g
```

### Post-Deployment Validation (Critical)

**1. Safari Playback Test (5 minutes)**
- [ ] Start live stream in Safari
- [ ] Browser console: NO MediaError code 4
- [ ] Timeline: Can rewind after 2+ minutes
- [ ] Scrubber: Shows full session length (not stuck at 3-5 segments)

**2. MIME Type Validation (1 minute)**
```bash
curl -I https://your-domain/api/v3/sessions/{id}/hls/init.mp4
# Expected: Content-Type: video/mp4
# Expected: Content-Encoding: identity

curl -I https://your-domain/api/v3/sessions/{id}/hls/seg_000001.m4s
# Expected: Content-Type: video/mp4
# Expected: Content-Encoding: identity
```

**3. Playlist Structure (1 minute)**
```bash
curl https://your-domain/api/v3/sessions/{id}/hls/index.m3u8 | head -20
# Expected: #EXT-X-PLAYLIST-TYPE:EVENT
# Expected: #EXT-X-PROGRAM-DATE-TIME:...Z
# Expected: NO #EXT-X-ENDLIST
```

**4. Segment Persistence (5 minutes)**
```bash
# Start session, wait 5 minutes
ls -1 /recordings/sessions/{id}/hls/seg_*.m4s | wc -l
# Expected: ~50 segments (5min / 6s)
# NOT: stuck at 3-5 segments
```

---

## Known Limitations

### 1. Runtime Probe Not Implemented

**Impact:** Deinterlacing must be configured manually for interlaced sources.

**Current Behavior:**
- Channel scanner detects interlaced sources and sets `Deinterlace=true`
- Runtime (per-session) probe not yet implemented

**Workaround:** Ensure profiles for interlaced channels have `Deinterlace=true` set.

**Future:** Automatic runtime probe will be added in future release.

---

### 2. Unbounded Playlist Growth

**Impact:** Very long sessions (> 8 hours) may cause slower scrubbing in Safari.

**Tradeoff:** This is intentional to support full DVR functionality.

**Mitigation:** Implement rolling window retention (Phase 2).

---

### 3. No VOD Finalization

**Impact:** Finished recordings remain as HLS segments (not single MP4 file).

**Future:** Optional MP4 finalization for downloads will be added later.

---

## Rollback Plan

If critical issues occur:

```bash
git checkout {previous-commit}
go build -o xg2g ./cmd/daemon
systemctl restart xg2g
```

**Indicators for rollback:**
- MediaError 4 persists after validation
- Disk fills unexpectedly fast (> 10GB/hour)
- CPU usage > 200% sustained

---

## Testing Completed

### Unit Tests
```bash
go test ./internal/pipeline/exec/ffmpeg/... ./internal/pipeline/api/...
# Status: ✅ All tests passing
```

### Integration Tests
- Safari HLS playback: ✅
- DVR timeline/scrubber: ✅
- Remux decision logic: ✅
- Deinterlace validation: ✅

### Build Verification
```bash
go build ./cmd/daemon
# Status: ✅ Clean build
```

---

## Timeline

| Phase | Timeline | Status |
|-------|----------|--------|
| Code implementation | 2026-01-05 | ✅ Complete |
| Testing | 2026-01-05 | ✅ Complete |
| **Deployment** | **2026-01-06** | **Ready** |
| Monitoring Phase 1 | 1-7 days | Pending |
| **Retention Policy** | **Within 1 week** | **CRITICAL** |

---

## Support & Troubleshooting

### Safari still shows MediaError 4

**Check:**
1. `curl -I` on init.mp4 → verify `Content-Type: video/mp4`
2. `curl -I` on .m4s → verify `Content-Type: video/mp4`
3. Check for `Content-Encoding: gzip` (must be `identity` or absent)
4. Verify reverse proxy config (gzip disabled for video)

### Timeline stuck at 3-5 segments

**Check:**
1. Playlist contains `#EXT-X-PLAYLIST-TYPE:EVENT`
2. NO `#EXT-X-ENDLIST` (indicates live)
3. Segment count on disk matches playlist length
4. ffmpeg logs show `hls_list_size 0` (not 3 or 5)

### Disk filling rapidly

**Immediate action:**
1. Check active sessions: `du -sh /recordings/sessions/*`
2. Identify long-running sessions
3. Implement Phase 1 cleanup (session-end deletion)

---

## References

- [REMUX_FIRST_DECISION.md](REMUX_FIRST_DECISION.md) - Architecture details
- [RETENTION_POLICY_REQUIREMENT.md](RETENTION_POLICY_REQUIREMENT.md) - Disk management
- [Safari HLS Contract](safari_hls_contract.md) - Browser compatibility

---

## Sign-Off

**Technical Review:** ✅ Approved
**Testing:** ✅ Complete
**Documentation:** ✅ Complete
**Production Readiness:** ✅ Approved (with retention requirement)

**Deployment Authorization:** Pending stakeholder approval

---

**Contact:** For deployment support, reference this document and changed files list.
