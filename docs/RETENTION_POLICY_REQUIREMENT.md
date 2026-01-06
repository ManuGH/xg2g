# Retention Policy Requirement (CRITICAL)

## Problem

With the implementation of **LIVE-DVR always** (no `delete_segments`), HLS segments now **persist indefinitely** until session cleanup.

**Consequences:**
- Disk usage grows unbounded during long sessions
- A 3-hour stream at 6s segments = 1800 segments
- At ~500KB/segment average = ~900MB per 3h session
- Without cleanup: **disk will fill**

---

## Required Implementation (Soon)

### Option 1: Session-End Cleanup (Simplest)

**Policy:** Delete all segments when session ends (no permanent VOD).

```go
// After session stops
sessionDir := filepath.Join(hlsRoot, "sessions", sessionID)
os.RemoveAll(sessionDir)
```

**Pros:**
- Simple, no retention logic
- Clean disk usage

**Cons:**
- No VOD playback after recording

---

### Option 2: DVR Window Enforcement (Recommended)

**Policy:** Keep only last N hours of segments (rolling window).

**Parameters:**
- `DVRWindowSec` (e.g., 7200 = 2 hours)
- Segment duration (6s default)
- Max segments = DVRWindowSec / 6

**Implementation:**

```go
// Pseudo-code for segment cleanup during active session
func cleanupOldSegments(sessionDir string, maxSegments int) {
    segments := listSegments(sessionDir) // sorted by sequence number
    if len(segments) > maxSegments {
        toDelete := segments[:len(segments)-maxSegments]
        for _, seg := range toDelete {
            os.Remove(seg)
        }
        // Update playlist to remove references to deleted segments
        updatePlaylistStartSequence(sessionDir, segments[len(segments)-maxSegments].SeqNum)
    }
}
```

**Playlist Update:**
```m3u8
#EXTM3U
#EXT-X-VERSION:7
#EXT-X-PLAYLIST-TYPE:EVENT
#EXT-X-MEDIA-SEQUENCE:1200  # Updated to first available segment
#EXT-X-INDEPENDENT-SEGMENTS
...
```

**Pros:**
- Predictable disk usage
- Still allows DVR/rewind within window
- Safari-compatible (EVENT playlist still valid)

**Cons:**
- More complex (playlist rewrite, sequence tracking)

---

### Option 3: VOD Finalization (Post-Session)

**Policy:** After recording ends, optionally remux segments into final MP4/MKV.

```bash
# After session ends, finalize to MP4
ffmpeg -i /recordings/{id}/hls/index.m3u8 \
  -c copy \
  -movflags faststart \
  /recordings/{id}/final.mp4

# Then delete HLS segments
rm -rf /recordings/{id}/hls/
```

**Pros:**
- Clean final file for download/library
- Smaller storage (single MP4 vs many segments)

**Cons:**
- Requires additional processing time
- Not streaming-ready (must wait for finalization)

---

## Recommended Strategy

**Phase 1: Session-End Cleanup (Deploy Now)**
- On session stop/expire: delete entire session directory
- Prevents disk fill during testing/development

**Phase 2: DVR Window (Deploy Soon)**
- Implement rolling window (2-4 hours recommended)
- Cleanup runs periodically (e.g., every 60 seconds)
- Only delete segments older than window

**Phase 3: VOD Finalization (Optional, Later)**
- Add opt-in "Save Recording" feature
- Remux to MP4 on user request
- Keep HLS for streaming, MP4 for download

---

## Disk Usage Calculations

| Session Duration | Segment Count | Disk Usage (avg 500KB/seg) |
|------------------|---------------|----------------------------|
| 30 minutes       | 300           | ~150 MB                    |
| 2 hours          | 1200          | ~600 MB                    |
| 4 hours          | 2400          | ~1.2 GB                    |
| 8 hours          | 4800          | ~2.4 GB                    |
| 24 hours         | 14400         | ~7.2 GB                    |

**With 10 concurrent streams @ 4h each:** ~12 GB

---

## Monitoring Requirements

### Metrics to Track

1. **Disk Usage per Session**
   ```bash
   du -sh /recordings/sessions/*
   ```

2. **Total Segment Count**
   ```bash
   find /recordings -name "seg_*.m4s" | wc -l
   ```

3. **Oldest Session Age**
   ```bash
   find /recordings/sessions -type d -mmin +60  # Sessions older than 1h
   ```

### Alerts

- Disk usage > 80% → Warning
- Disk usage > 90% → Critical
- Session directory > 5GB → Investigate

---

## Implementation Checklist

- [ ] **Phase 1: Immediate (Deploy with DVR fix)**
  - [ ] Add session-end cleanup hook
  - [ ] Test: session dir deleted after stop
  - [ ] Monitor disk usage in production

- [ ] **Phase 2: Soon (Within 1-2 weeks)**
  - [ ] Implement DVR window enforcement
  - [ ] Add periodic cleanup task (every 60s)
  - [ ] Update playlist #EXT-X-MEDIA-SEQUENCE
  - [ ] Test: segments older than window are deleted

- [ ] **Phase 3: Optional (Future)**
  - [ ] Add "Save Recording" API endpoint
  - [ ] Implement MP4 finalization
  - [ ] Add download endpoint

---

## Sample Implementation: Session-End Cleanup

```go
// In orchestrator.go or cleanup handler
func (o *Orchestrator) cleanupSession(sessionID string) error {
    sessionDir := filepath.Join(o.hlsRoot, "sessions", sessionID)

    log.L().Info().
        Str("session_id", sessionID).
        Str("path", sessionDir).
        Msg("cleaning up session directory")

    if err := os.RemoveAll(sessionDir); err != nil {
        log.L().Error().Err(err).
            Str("session_id", sessionID).
            Msg("failed to cleanup session directory")
        return err
    }

    return nil
}
```

Call this when session state transitions to terminal (STOPPED, FAILED, CANCELLED).

---

## Testing

### Manual Test: Verify Cleanup

```bash
# Start session
curl -X POST http://localhost:8080/api/v3/intents -d '{...}'

# Check directory exists
ls -lh /recordings/sessions/{session-id}/

# Stop session
curl -X POST http://localhost:8080/api/v3/sessions/{id}/stop

# Verify directory deleted
ls /recordings/sessions/{session-id}/
# Expected: No such file or directory
```

### Automated Test: DVR Window

```go
func TestDVRWindow_SegmentCleanup(t *testing.T) {
    // Create 100 segments
    // Set DVR window to 30 segments
    // Run cleanup
    // Assert: only last 30 segments remain
    // Assert: playlist updated with correct MEDIA-SEQUENCE
}
```

---

## References

- [REMUX_FIRST_DECISION.md](REMUX_FIRST_DECISION.md) – Storage layout
- [args.go](../internal/pipeline/exec/ffmpeg/args.go) – HLS playlist configuration
- [orchestrator.go](../internal/pipeline/worker/orchestrator.go) – Session lifecycle

---

**Action Required:** Implement Phase 1 (session-end cleanup) BEFORE deploying to production with unbounded DVR.
