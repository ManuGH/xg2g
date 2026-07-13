#!/usr/bin/env bash
# quality-gate.sh — Golden-sample encode gate for the xg2g transcode pipeline.
#
# Synthesizes a deterministic 1080i50 BT.709 source (the pipeline's hardest
# common case), runs it through a production-shaped deinterlace+encode, and
# fails the build when pipeline invariants break:
#
#   1. avg encode speed >= MIN_SPEED   (encoder must hold realtime)
#   2. dropped frames == 0
#   3. duplicated frames <= MAX_DUP
#   4. output DAR is 16:9
#   5. output is 50 fps progressive
#   6. BT.709 color metadata survives (primaries/transfer/matrix)
#   7. audio/video duration delta <= 250 ms
#
# Defaults target CI (software x264). On a VAAPI host run e.g.:
#   XG2G_QG_ENCODER=h264_vaapi XG2G_QG_VAAPI_DEVICE=/dev/dri/renderD128 scripts/quality-gate.sh
#   XG2G_QG_ENCODER=av1_vaapi  XG2G_QG_MIN_SPEED=1.02 scripts/quality-gate.sh
set -euo pipefail

ENCODER="${XG2G_QG_ENCODER:-libx264}"
DURATION="${XG2G_QG_DURATION:-8}"
MIN_SPEED="${XG2G_QG_MIN_SPEED:-1.02}"
MAX_DUP="${XG2G_QG_MAX_DUP:-0}"
ENFORCE_SPEED="${XG2G_QG_ENFORCE_SPEED:-1}"
VAAPI_DEVICE="${XG2G_QG_VAAPI_DEVICE:-/dev/dri/renderD128}"
WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/xg2g-quality-gate.XXXXXX")"
trap 'rm -rf "$WORKDIR"' EXIT

SOURCE="$WORKDIR/golden_1080i50.ts"
# AV1 rides in fMP4/CMAF in production and mpegts probing is unreliable for it;
# H.264/HEVC gate output stays mpegts like the live pipeline.
case "$ENCODER" in
  *av1*) OUTPUT="$WORKDIR/encoded.mp4"; OUTPUT_FORMAT="mp4" ;;
  *)     OUTPUT="$WORKDIR/encoded.ts";  OUTPUT_FORMAT="mpegts" ;;
esac
ENCODE_LOG="$WORKDIR/encode.log"

fail() { echo "❌ QUALITY GATE FAILED: $*" >&2; exit 1; }
note() { echo "▸ $*"; }

command -v ffmpeg >/dev/null || fail "ffmpeg not found"
command -v ffprobe >/dev/null || fail "ffprobe not found"

# --- 1. Golden sample: 1080i50 TFF, BT.709 tagged, moving content + audio ---
note "Synthesizing golden 1080i50 sample (${DURATION}s)"
ffmpeg -hide_banner -loglevel error -y \
  -f lavfi -i "testsrc2=size=1920x1080:rate=50" \
  -f lavfi -i "sine=frequency=440:sample_rate=48000" \
  -t "$DURATION" \
  -vf "tinterlace=mode=interleave_top,fieldorder=tff,setdar=16/9,format=yuv420p" \
  -color_primaries bt709 -color_trc bt709 -colorspace bt709 \
  -c:v libx264 -preset ultrafast -crf 18 -x264opts tff=1 \
  -c:a aac -b:a 128k \
  -f mpegts "$SOURCE"

# --- 2. Production-shaped encode: bwdif field-rate deinterlace to 50p ---
# setparams stamps BT.709 onto the frames so every encoder writes it into the
# bitstream VUI; output-level -color_* flags alone only tag the container.
DEINT_FILTER="bwdif=mode=send_field:parity=auto:deint=all,setparams=color_primaries=bt709:color_trc=bt709:colorspace=bt709"
case "$ENCODER" in
  *_vaapi)
    note "Encoding via $ENCODER on $VAAPI_DEVICE"
    ENCODE_ARGS=(
      -init_hw_device "vaapi=va:$VAAPI_DEVICE" -filter_hw_device va
      -i "$SOURCE"
      -vf "${DEINT_FILTER},format=nv12,hwupload"
      -c:v "$ENCODER" -b:v 12M
    )
    ;;
  libsvtav1)
    note "Encoding via libsvtav1 (software AV1)"
    ENCODE_ARGS=(
      -i "$SOURCE"
      -vf "${DEINT_FILTER},format=yuv420p10le"
      -c:v libsvtav1 -preset 10 -crf 32
    )
    ;;
  *)
    note "Encoding via $ENCODER (software)"
    ENCODE_ARGS=(
      -i "$SOURCE"
      -vf "${DEINT_FILTER},format=yuv420p"
      -c:v "$ENCODER" -preset veryfast -crf 21
    )
    ;;
esac

ffmpeg -hide_banner -y "${ENCODE_ARGS[@]}" \
  -color_primaries bt709 -color_trc bt709 -colorspace bt709 \
  -c:a aac -b:a 160k \
  -f "$OUTPUT_FORMAT" "$OUTPUT" 2> "$ENCODE_LOG" || { cat "$ENCODE_LOG" >&2; fail "encode failed"; }

# --- 3. Speed / drop / dup from ffmpeg's final progress line ---
FINAL_LINE="$(grep -Eo 'frame=.*speed=[0-9.]+x' "$ENCODE_LOG" | tail -1 || true)"
[ -n "$FINAL_LINE" ] || fail "could not parse ffmpeg progress line"
SPEED="$(sed -nE 's/.*speed=\s*([0-9.]+)x.*/\1/p' <<<"$FINAL_LINE")"
DUP="$(sed -nE 's/.*dup=\s*([0-9]+).*/\1/p' <<<"$FINAL_LINE")"; DUP="${DUP:-0}"
DROP="$(sed -nE 's/.*drop=\s*([0-9]+).*/\1/p' <<<"$FINAL_LINE")"; DROP="${DROP:-0}"
note "speed=${SPEED}x dup=${DUP} drop=${DROP}"

if [ "$ENFORCE_SPEED" = "1" ]; then
  awk -v s="$SPEED" -v min="$MIN_SPEED" 'BEGIN { exit (s+0 >= min+0) ? 0 : 1 }' \
    || fail "encode speed ${SPEED}x below required ${MIN_SPEED}x"
else
  note "speed gate not enforced (XG2G_QG_ENFORCE_SPEED=$ENFORCE_SPEED)"
fi
[ "$DROP" -eq 0 ] || fail "dropped frames: $DROP (expected 0)"
[ "$DUP" -le "$MAX_DUP" ] || fail "duplicated frames: $DUP (allowed: $MAX_DUP)"

# --- 4-6. ffprobe invariants on the encoded output ---
probe() {
  ffprobe -v error -select_streams "$1" -show_entries "stream=$2" \
    -of default=noprint_wrappers=1:nokey=1 "$OUTPUT" | head -1
}

DAR="$(probe v:0 display_aspect_ratio)"
if [ -z "$DAR" ] || [ "$DAR" = "N/A" ] || [ "$DAR" = "0:1" ]; then
  # Some codecs (AV1 in TS) omit the DAR tag; derive it from the geometry.
  WIDTH="$(probe v:0 width)"; HEIGHT="$(probe v:0 height)"
  awk -v w="$WIDTH" -v h="$HEIGHT" 'BEGIN { exit (w*9 == h*16) ? 0 : 1 }' \
    || fail "geometry ${WIDTH}x${HEIGHT} is not 16:9 and no DAR tag present"
  DAR="16:9 (derived ${WIDTH}x${HEIGHT})"
else
  [ "$DAR" = "16:9" ] || fail "display aspect ratio is '$DAR' (expected 16:9)"
fi

RATE="$(probe v:0 r_frame_rate)"
[ "$RATE" = "50/1" ] || fail "output frame rate is '$RATE' (expected 50/1)"

FIELD_ORDER="$(probe v:0 field_order)"
case "$FIELD_ORDER" in
  progressive|unknown|"") : ;;
  *) fail "output field_order is '$FIELD_ORDER' (expected progressive)" ;;
esac

for entry in color_primaries color_transfer color_space; do
  VALUE="$(probe v:0 "$entry")"
  [ "$VALUE" = "bt709" ] || fail "$entry is '$VALUE' (expected bt709)"
done

# --- 7. A/V duration alignment ---
VDUR="$(ffprobe -v error -select_streams v:0 -show_entries stream=duration -of default=noprint_wrappers=1:nokey=1 "$OUTPUT" | head -1)"
ADUR="$(ffprobe -v error -select_streams a:0 -show_entries stream=duration -of default=noprint_wrappers=1:nokey=1 "$OUTPUT" | head -1)"
if [ -n "$VDUR" ] && [ -n "$ADUR" ] && [ "$VDUR" != "N/A" ] && [ "$ADUR" != "N/A" ]; then
  awk -v v="$VDUR" -v a="$ADUR" 'BEGIN { d = v - a; if (d < 0) d = -d; exit (d <= 0.25) ? 0 : 1 }' \
    || fail "A/V duration delta exceeds 250ms (video=${VDUR}s audio=${ADUR}s)"
  note "A/V duration delta OK (video=${VDUR}s audio=${ADUR}s)"
else
  note "WARN: container did not report per-stream durations; skipping A/V delta gate"
fi

echo "✅ QUALITY GATE PASSED (encoder=$ENCODER speed=${SPEED}x dar=$DAR rate=$RATE)"
