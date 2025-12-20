# Rust Audio Remuxer Integration

This guide details the integration of the high-performance Rust Audio Remuxer into xg2g, including build instructions, configuration priority, and verification steps.

## Prerequisites

To build the Rust-enabled binary (`xg2g-ffi`), your environment must have:

1. **Rust Toolchain**: `rustc` and `cargo` (Latest Stable: **1.92.0**, Minimum: 1.70+).
    - Install via rustup: `curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh`
2. **C Compiler**: `gcc` or `clang` (for CGO linking).
3. **Go Toolchain**: Go **1.25+** (Latest Stable: **1.25.5**) with `CGO_ENABLED=1`.
4. **FFmpeg**: Version **8.0+** (Latest Stable: **8.0.1**) recommended for H.264 Repair.

## Build Instructions

We provide a dedicated Makefile target to build the binary with the embedded Rust library:

```bash
# Build the Rust shared library and link it into the Go binary
make build-ffi
```

This produces the binary at `bin/xg2g-ffi`.

## Configuration & Priority Logic

Since version 3.2, xg2g uses a strict priority system to choose the best transcoding strategy for each stream.

### Priority Order (Enforced in Code)

1. **Rust Audio Remuxer**: `XG2G_USE_RUST_REMUXER=true` (Default)
    - **Priority**: High (Speed/Efficiency).
    - **Use Case**: WebUI, iOS, Safari, VLC. Ideal for clients that tolerate raw H.264 streams but need AAC audio.
    - **Performance**: Extremely low latency (~0ms start), negligible CPU (<1MB RAM).

2. **H.264 Stream Repair (FFmpeg)**: `XG2G_H264_STREAM_REPAIR=true`
    - **Priority**: Medium (Plex Only).
    - **Use Case**: Required for Plex, Jellyfin, and strict clients that choke on raw Enigma2 streams (missing PPS/SPS headers).
    - **Condition**: Only active if the Rust Remuxer is **disabled** or not applicable for the stream.
    - **Caveat**: If enabled, this **disables** the Rust Remuxer for that stream, because we must use FFmpeg to repair the video.
    - **Performance**: Higher CPU usage / latency (starts `ffmpeg` process).

3. **FFmpeg Transcoding**: (Fallback)
    - **Priority**: Low.
    - **Use Case**: If Rust fails or codecs are incompatible.

### Supported Streaming Modes

| Feature | Direct Stream (VLC/IPTV) | HLS (WebUI/iOS) |
| :--- | :--- | :--- |
| **Rust Remuxer** | ✅ **Supported** (Default) | ❌ **Not Supported** (Requires FFmpeg) |
| **H.264 Repair** | ⚠️ Only if Rust Disabled | ✅ **Implicitly Handled via HLS** |
| **FFmpeg Transcode** | ✅ Fallback | ✅ Standard |

> [!NOTE]
> **Why no Rust for HLS?**
> The Rust logic is a high-speed linear remuxer (Stream A -> Stream B).
> HLS requires complex "Segmentation" (chopping the stream into files, managing playlists, timestamp alignment). This is currently only possible with FFmpeg.
> Therefore, **WebUI and iOS (Auto-HLS) will always use FFmpeg**, regardless of priority settings.

### Recommended Configurations

#### Scenario A: "I only use WebUI / iOS" (Recommended)

- `XG2G_USE_RUST_REMUXER=true`
- **Result**: Maximum speed and efficiency.

#### Scenario B: "I use Plex heavily"

- `XG2G_H264_STREAM_REPAIR=true`
- **Result**: Streams are compatible with Plex. Rust Remuxer is effectively disabled for video streams to ensure integrity (Safety > Speed).

## Smoke Test (Verification)

To verify which path is being taken, observe the logs at `DEBUG` or `INFO` level.

1. **Start the Daemon**:

    ```bash
    ./bin/xg2g-ffi
    ```

2. **Request a Stream**: Open a stream in the WebUI or VLC.

3. **Check Logs**:

    - **If Rust is active**:

        ```json
        {"level":"info","method":"rust","msg":"attempting native rust remuxer",...}
        {"level":"info","msg":"rust remuxer initialized","sample_rate":48000,...}
        ```

    - **If Repair is active**:

        ```json
        {"level":"info","msg":"routing stream through H.264 PPS/SPS repair (FFmpeg)",...}
        ```

## Troubleshooting

- **Error**: `error while loading shared libraries: libxg2g_transcoder.so: cannot open shared object file`
  - **Fix**: Ensure `LD_LIBRARY_PATH` includes the directory containing the Rust library (e.g., `export LD_LIBRARY_PATH=$(pwd)/transcoder/target/release`).
