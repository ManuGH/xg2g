#!/usr/bin/env bash
set -euo pipefail

# FFmpeg Dual-Rendition HLS Generator Spike (P6.1b)
# Target: 720p (2.0Mbps) + 480p (0.9Mbps), 25fps, 2.0s synchronized GOPs

OUT_DIR="${1:-/tmp/xg2g_abr_p61b}"
PID_FILE="${OUT_DIR}/ffmpeg.pid"

mkdir -p "${OUT_DIR}/720p" "${OUT_DIR}/480p"

echo "Starting FFmpeg Dual-Rendition HLS process into ${OUT_DIR}..."

# Run FFmpeg continuously with synthetic test input (video testsrc2 + audio sine)
# Per-stream video parameters guarantee identical 25fps, GOP 50, and 2-second forced keyframes on both renditions.
ffmpeg -hide_banner -loglevel info -y \
  -f lavfi -i "testsrc2=size=1920x1080:rate=25" \
  -f lavfi -i "sine=frequency=440:sample_rate=48000" \
  -filter_complex "[0:v]split=2[v1out][v2out]; [v1out]scale=1280:720[v1]; [v2out]scale=854:480[v2]" \
  -map "[v1]" -c:v:0 libx264 -preset:v:0 veryfast \
    -r:v:0 25 -g:v:0 50 -keyint_min:v:0 50 -sc_threshold:v:0 0 \
    -force_key_frames:v:0 "expr:gte(t,n_forced*2)" \
    -b:v:0 2000k -maxrate:v:0 2400k -bufsize:v:0 4000k \
  -map "[v2]" -c:v:1 libx264 -preset:v:1 veryfast \
    -r:v:1 25 -g:v:1 50 -keyint_min:v:1 50 -sc_threshold:v:1 0 \
    -force_key_frames:v:1 "expr:gte(t,n_forced*2)" \
    -b:v:1 900k -maxrate:v:1 1100k -bufsize:v:1 1800k \
  -map 1:a -c:a:0 aac -b:a:0 128k -ar:a:0 48000 -ac:a:0 2 \
  -map 1:a -c:a:1 aac -b:a:1 128k -ar:a:1 48000 -ac:a:1 2 \
  -f hls \
  -hls_time 2 \
  -hls_playlist_type event \
  -hls_flags independent_segments \
  -hls_segment_filename "${OUT_DIR}/%v/seq_%05d.ts" \
  -master_pl_name master.m3u8 \
  -var_stream_map "v:0,a:0,name:720p v:1,a:1,name:480p" \
  "${OUT_DIR}/%v/index.m3u8" > "${OUT_DIR}/ffmpeg.log" 2>&1 &

FFMPEG_PID=$!
echo "${FFMPEG_PID}" > "${PID_FILE}"
echo "FFmpeg started with PID ${FFMPEG_PID}"
