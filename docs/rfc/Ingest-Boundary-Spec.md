# Ingest Boundary Specification

This document defines the technical requirements for the xg2g `worker` and `ffmpeg` runner to enforce the Zero-Trust boundary with Enigma2 sources as defined in [ADR-004](file:///root/xg2g/docs/rfc/ADR-004-enigma2-trust-boundary.md).

## 1. FFmpeg Configuration (The "Repair Shop")

To isolate xg2g from Enigma2's "actively wrong" timing, the following FFmpeg flags and behaviors are mandatory.

### 1.1 Input Flags

- **`-fflags +genpts+igndts+ignidx`**: Generate new PTS, ignore source DTS and index.
- **`-err_detect ignore_err`**: Prevent decoding errors in the noisy source from terminating the transcoder prematurely. **Note**: This is limited to the input level; decode errors must still be monitored.
- **`-use_wallclock_as_timestamps 1`**: **Experimental/Opt-in Only**. Do not use as default due to potential massive jumps on source stall.

### 1.2 Global Mapping & Normalization

- **`-avoid_negative_ts make_zero`**: Ensure all timestamps start at 0 and stay positive.
- **`-flags +global_header`**: Crucial for fMP4 and HEVC compatibility.
- **GOP Stability**: Do NOT use `-r` (resampling) by default to keep CPU usage low. Focus on enforcing constant GOP boundaries instead. Resampling is only a fallback for extreme drift.

### 1.3 GOP & Segmentation

- **`-g <gop_size>`**: Enforce fixed GOP size (calculated based on target segment duration).
- **`-sc_threshold 0`**: Disable scene-cut detection to ensure deterministic GOP boundaries.
- **`-force_key_frames expr:gte(t,n_forced*<dur>)`**: Hard-coded keyframe intervals.

---

## 2. Worker Lifecycle & Conflict Handling

The `internal/pipeline/worker` must act as the orchestrator of the boundary.

### 2.1 Hard Resets

The worker must monitor the stderr of FFmpeg for changes and trigger a **Hard Reset** (Stop/Start with new rendition):

- **Resolution Change**: e.g., "Input stream resolution changed".
- **Codec Change**: e.g., "Input stream codec changed".
- **Relevant PMT Change**: Trigger reset only if PMT version change affects Video PIDs or Codecs. Ignore uncritical metadata-only updates.

### 2.2 No Soft Resume

After a Hard Reset, xg2g MUST NOT reuse old state:

- New Timeline.
- Reset HLS Sequence.
- Fresh Encoder State.
Every reset produces a clean cut for the player.

### 2.3 Lease > Transport

A stream lives as long as the xg2g-Lease lives, not just as long as the TCP connection is open.

### 2.4 Health Monitoring

- **Ingest Stalls**: If the source socket provides no data for > 5 seconds, Terminate Session. Do NOT attempt "soft-reconnect" inside the same pipeline rendition.
- **PTS Drifts**: Monitor emitted segments. If segment duration drift exceeds 500ms, Log warning and increment `enigma_pts_drift_total`.

---

## 3. Metrics (Boundary Telemetry)

The following metrics must be implemented to measure source reliability:

| Metric | Type | Description |
| :--- | :--- | :--- |
| `enigma_pts_jump_total` | Counter | Number of times a source PTS jump was detected/ignored |
| `enigma_pmt_change_total` | Counter | Number of PMT changes detected |
| `enigma_ingest_reset_total` | Counter | Number of hard resets triggered by source instability |
| `enigma_source_stall_total` | Counter | Number of times source connection stalled |
| `enigma_decode_error_total` | Counter | Number of decode errors/corrupt packets ignored via `ignore_err` |

---

## 4. Implementation Guidance

1. **FFmpeg Runner**: Update `BuildHLSArgs` in `internal/pipeline/exec/ffmpeg/args.go` to strictly follow these flags.
2. **Supervisor**: Enhance the FFmpeg stdout/stderr parser in `runner.go` to detect resolution/codec changes.
3. **Internal Origin**: Hard-code source timeouts to be more aggressive than downstream player timeouts.
