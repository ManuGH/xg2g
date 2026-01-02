# VOD Operator Runbook (V3)

This document describes operational procedures for the Direct VOD playback system.

## 1. Playback States (What the user sees)

| State | User Experience | Meaning |
|-------|-----------------|---------|
| **Buffering (HLS)** | User sees loading spinner, then plays. | MP4 build is in progress. Player fell back to HLS for instant start. |
| **Direct Play** | Instant start, fast seeking. | MP4 cache exists and is being served directly. |
| **Error** | "Recording not available". | Source not found or permission error. |

## 2. Common Logs & Meanings

| Log Message | Level | Meaning | Action |
|-------------|-------|---------|--------|
| `starting vod remux` | INFO | New MP4 cache building. | Normal. |
| `vod build in progress, returning 503` | DEBUG | Client asked for MP4 while building. | Normal (Client handles fallback). |
| `removing stale vod lock` | WARN | Found a lock > 30 mins old. | System is self-healing after a crash. Normal. |
| `disk pressure detected` | WARN | Data volume has < 5GB free. | System triggers aggressive cleanup. **Check Disk Usage.** |
| `evicting cache item due to disk pressure` | WARN | Deleting old files to free space. | **Alert**: Add disk space if this happens often. |

## 3. Storage Management

* **Path**: `data/vod-cache` (MP4 files) and `data/hls/recordings` (HLS HLS).
* **Policy**:
  * **Normal**: Files deleted after 24h (Config: `VODCacheTTL`).
  * **Pressure**: If free space < 5GB, files deleted immediately (LRU) until 1GB is freed.
* **Reset**: To clear all caches manually, stop the server and `rm -rf data/vod-cache/*`.

## 4. Troubleshooting

**Q: Why is it puffering?**
A: If it's the *first* time playing this recording, the system might be falling back to HLS if the MP4 isn't ready. HLS has inherent latency.

**Q: Why does it say "Preparing" forever?**
A: Check logs for `vod remux failed`. If FFmpeg fails (e.g., corrupt source), the lock is removed and it might loop.

* Check source file integrity.
* Check `dev.log` for FFmpeg errors (`grep "vod remux failed"`).

**Q: Disk is full!**
A: The system should self-heal. Check logs for `disk pressure detected`.
If not self-healing, check if `dataDir` is correctly mounted and writable.

**Q: Server rebooted, what happens to builds?**
A:

* Running builds die (cleanly).
* Lock files are removed on next startup (`CleanupStaleArtifacts`).
* Next request restarts the build.
