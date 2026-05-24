#!/usr/bin/env bash
# Generates a tiny self-contained HLS VOD fixture (test pattern + tone, ~4s)
# for the playback smoke. Runs inside the e2e container build — NOT on the host.
# Output: backend/e2e/fixtures/hls/{index.m3u8,seg_*.ts}
set -euo pipefail

OUT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/fixtures/hls"
mkdir -p "$OUT_DIR"

if [[ -f "$OUT_DIR/index.m3u8" && "${FORCE:-0}" != "1" ]]; then
  echo "HLS fixture already present at $OUT_DIR (set FORCE=1 to regenerate)."
  exit 0
fi

# Non-fatal if ffmpeg is missing: the boot smoke must still run, and the
# real-hls.js playback spec skips itself when the fixture is absent (see
# specs/playback.spec.ts). CI installs ffmpeg so the spec actually runs there.
if ! command -v ffmpeg >/dev/null 2>&1; then
  echo "WARN: ffmpeg not found — skipping HLS fixture generation; the real-hls.js playback spec will be skipped." >&2
  exit 0
fi

ffmpeg -y -hide_banner -loglevel error \
  -f lavfi -i "testsrc=size=320x180:rate=15:duration=4" \
  -f lavfi -i "sine=frequency=440:duration=4" \
  -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 15 \
  -c:a aac -b:a 64k \
  -f hls -hls_time 1 -hls_list_size 0 -hls_playlist_type vod \
  -hls_segment_filename "$OUT_DIR/seg_%03d.ts" \
  "$OUT_DIR/index.m3u8"

echo "Generated HLS fixture:"
ls -la "$OUT_DIR"
