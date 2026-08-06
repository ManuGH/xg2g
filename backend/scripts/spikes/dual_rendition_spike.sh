#!/usr/bin/env bash
set -euo pipefail

# Dual-Rendition HLS Transcoding Spike Script
# Strict software encoding: libx264 + AAC
# Output format: Master playlist (index.m3u8) + variant playlists (variant_high/index.m3u8, variant_low/index.m3u8)

OUT_DIR=""
FFMPEG_BIN="$(command -v ffmpeg || true)"
FFPROBE_BIN="$(command -v ffprobe || true)"
DURATION_SEC=10
IS_LIVE=0

usage() {
    echo "Usage: $0 --output-dir <path> [--ffmpeg <path>] [--ffprobe <path>] [--duration <sec>] [--live]"
    exit 1
}

while [[ $# -gt 0 ]]; do
    case "$1" in
        --output-dir)
            OUT_DIR="$2"
            shift 2
            ;;
        --ffmpeg)
            FFMPEG_BIN="$2"
            shift 2
            ;;
        --ffprobe)
            FFPROBE_BIN="$2"
            shift 2
            ;;
        --duration)
            DURATION_SEC="$2"
            shift 2
            ;;
        --live)
            IS_LIVE=1
            shift 1
            ;;
        *)
            echo "Unknown argument: $1"
            usage
            ;;
    esac
done

if [[ -z "$OUT_DIR" ]]; then
    echo "Error: --output-dir is required"
    usage
fi

if [[ -z "$FFMPEG_BIN" || ! -x "$FFMPEG_BIN" ]]; then
    echo "Error: ffmpeg binary not found or not executable: $FFMPEG_BIN"
    exit 1
fi

if [[ -z "$FFPROBE_BIN" || ! -x "$FFPROBE_BIN" ]]; then
    echo "Error: ffprobe binary not found or not executable: $FFPROBE_BIN"
    exit 1
fi

mkdir -p "$OUT_DIR"
mkdir -p "$OUT_DIR/logs"

LOG_FILE="$OUT_DIR/logs/ffmpeg_stderr.log"

echo "==> Starting Dual-Rendition HLS Spike (Live=$IS_LIVE)..."
echo "  Output Directory: $OUT_DIR"
echo "  FFmpeg Binary:    $FFMPEG_BIN"
echo "  FFprobe Binary:   $FFPROBE_BIN"

if [[ "$IS_LIVE" -eq 1 ]]; then
    echo "  Mode:             Live Continuous Stream (-re, sliding window)"
    # Live mode: exec FFmpeg directly so script PID becomes FFmpeg PID
    exec "$FFMPEG_BIN" -y -hide_banner \
        -re -f lavfi -i "testsrc=size=1280x720:rate=25" \
        -re -f lavfi -i "sine=frequency=1000:sample_rate=44100" \
        -filter_complex "
          [0:v]split=2[vhigh_in][vlow_in];
          [vhigh_in]scale=w=1280:h=720:force_original_aspect_ratio=decrease,
                     pad=1280:720:(ow-iw)/2:(oh-ih)/2[vhigh];
          [vlow_in]scale=w=640:h=360:force_original_aspect_ratio=decrease,
                   pad=640:360:(ow-iw)/2:(oh-ih)/2[vlow]
        " \
        -map "[vhigh]" -c:v:0 libx264 -b:v:0 2000k -r:v:0 25 -g:v:0 50 -keyint_min:v:0 50 -sc_threshold:v:0 0 \
        -map "[vlow]"  -c:v:1 libx264 -b:v:1 600k  -r:v:1 25 -g:v:1 50 -keyint_min:v:1 50 -sc_threshold:v:1 0 \
        -map 1:a:0 -c:a:0 aac -b:a:0 128k \
        -map 1:a:0 -c:a:1 aac -b:a:1 64k \
        -f hls \
        -hls_time 2 \
        -hls_list_size 10 \
        -hls_flags delete_segments+independent_segments \
        -master_pl_name index.m3u8 \
        -var_stream_map "v:0,a:0,name:high v:1,a:1,name:low" \
        -hls_segment_filename "$OUT_DIR/variant_%v/seg_%05d.ts" \
        "$OUT_DIR/variant_%v/index.m3u8" 2> "$LOG_FILE"
else
    echo "  Duration:         ${DURATION_SEC}s"
    "$FFMPEG_BIN" -y -hide_banner \
        -f lavfi -i "testsrc=size=1280x720:rate=25:duration=${DURATION_SEC}" \
        -f lavfi -i "sine=frequency=1000:sample_rate=44100:duration=${DURATION_SEC}" \
        -filter_complex "
          [0:v]split=2[vhigh_in][vlow_in];
          [vhigh_in]scale=w=1280:h=720:force_original_aspect_ratio=decrease,
                     pad=1280:720:(ow-iw)/2:(oh-ih)/2[vhigh];
          [vlow_in]scale=w=640:h=360:force_original_aspect_ratio=decrease,
                   pad=640:360:(ow-iw)/2:(oh-ih)/2[vlow]
        " \
        -map "[vhigh]" -c:v:0 libx264 -b:v:0 2000k -r:v:0 25 -g:v:0 50 -keyint_min:v:0 50 -sc_threshold:v:0 0 \
        -map "[vlow]"  -c:v:1 libx264 -b:v:1 600k  -r:v:1 25 -g:v:1 50 -keyint_min:v:1 50 -sc_threshold:v:1 0 \
        -map 1:a:0 -c:a:0 aac -b:a:0 128k \
        -map 1:a:0 -c:a:1 aac -b:a:1 64k \
        -f hls \
        -hls_time 2 \
        -hls_playlist_type vod \
        -hls_flags independent_segments \
        -master_pl_name index.m3u8 \
        -var_stream_map "v:0,a:0,name:high v:1,a:1,name:low" \
        -hls_segment_filename "$OUT_DIR/variant_%v/seg_%05d.ts" \
        "$OUT_DIR/variant_%v/index.m3u8" 2> "$LOG_FILE"

    echo "==> FFmpeg execution completed successfully."
    echo "==> Performing ffprobe sanity check on generated master playlist..."
    "$FFPROBE_BIN" -v error "$OUT_DIR/index.m3u8"
    echo "==> Output written to: $OUT_DIR"
fi
