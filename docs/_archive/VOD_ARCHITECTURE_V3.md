# VOD Architecture V3: Direct Playback & Self-Healing

**Status**: Production Ready (Operator Grade)
**Version**: 1.2

## 1. Core Architecture

The V3 VOD system implements a hybrid playback strategy designed for instant start, native seek performance, and minimal server load.

### Strategy Selection (`GetRecordingPlaybackInfo`)

1. **Direct MP4 (`direct_mp4`)**:
    * **Condition**: Local file access + File is "stable" (no growth in 2s) + Client supports MP4.
    * **Mechanism**: On-demand remux (container swap) from `.ts` to `.mp4` (fragmented, faststart).
    * **Benefit**: Zero transcoding cost (video copy), native browser seek interactions, persistent cache.
2. **HLS Fallback (`hls`)**:
    * **Condition**: Remote file or Active Recording (growing) or **Cache Build in Progress**.
    * **Mechanism**: Standard FFmpeg HLS segmentation.

## 2. Design Principles (Hardening)

To ensure operational stability without manual intervention, the system adheres to strict self-healing principles:

### A. Systemic Safety (No Disk-Full Failures)

* **Proactive Disk Pressure Monitoring**: Uses `syscall.Statfs` to monitor the Data Volume.
* **Aggressive Eviction**: If free space falls below **5GB**:
  * Triggers immediate eviction loop.
  * Target: Free at least **1GB** immediately.
  * Strategy: Deletes oldest inactive items (HLS directories or MP4 files) regardless of TTL.
* **Unified Cache Model**: A single eviction logic handles both `recordings/` (HLS) and `vod-cache/` (MP4) using Last-Access-Time (LRU).

### B. Concurrency & Build Safety

* **Atomic Locking**: Uses `O_EXCL` for lock files. Guarantees exactly one remux process per recording.
* **Stale Lock Recovery**:
  * Detects lock files older than **30 minutes** (crash residue).
  * Automatically removes stale locks and allows new requests to take over.
* **Active Build Protection**: Eviction logic explicitly checks active build maps (`recordingRun`) and lock file existence before deletion. Active streams are **never** evicted.

### C. Lifecycle Management

* **Root Context Binding**: All background remux jobs inherit from the Server's Root Context.
* **Clean Shutdown**: Server shutdown triggers immediate cancellation of all FFmpeg processes, preventing zombies.
* **Non-Blocking Telemetry**: FFmpeg progress parsing uses non-blocking channels. If the server is under load, telemetry frames are dropped rather than stalling the transcoding process.

## 3. Playback Flow (Hybrid Strategy)

1. **Client Request**: `GET /api/v3/recordings/{id}/playback`
2. **Decision**:
    * If Local & Stable -> Return `direct_mp4` URL.
    * Else -> Return `hls` URL.
3. **Direct Stream Request**: `GET /.../stream.mp4`
    * **Cache Hit**: Serve via `http.ServeContent` (Range requests supported).
    * **Cache Miss**:
        * Acquire Lock (Atomic).
        * Launch Background Remux (`ffmpeg -c:v copy -c:a aac ...`).
        * Return `503 Service Unavailable`.
    * **Hybrid Fallback (Client)**:
        * Client receives `503`.
        * Client logs "Direct MP4 building, falling back to HLS".
        * Client plays HLS stream (`/playlist.m3u8`) **immediately**.
        * MP4 builds in background via Server Root Context.
        * **Next Play**: MP4 is ready -> Direct Play.

## 4. Operational Metrics

* **Logs**:
  * `starting vod remux`: Start of a new cache build.
  * `removing stale vod lock`: Self-healing action triggered.
  * `disk pressure detected`: Warning that aggressive eviction is active.
  * `evicting stale recording cache`: Normal TTL cleanup.
* **Monitoring**:
  * Watch `free_bytes` in logs.
  * Correlation between `503` spikes and `starting vod remux` (cache formations).

## 5. Future Optimizations (Non-Critical)

* **Telemetry**: Export MP4 cache size and eviction counters to Prometheus.
* **Tuning**: Configurable Disk Pressure thresholds (currently hardcoded 5GB/1GB).
* **UX**: Client-side indicator for "Direct Play" vs "Transcode".

---
**Conclusion**: This architecture shifts VOD from "experimental" to "reliable infrastructure", capable of running unattended without consuming all available resources.
