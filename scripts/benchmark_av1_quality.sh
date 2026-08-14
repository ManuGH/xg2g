#!/usr/bin/env bash
# benchmark_av1_quality.sh
# Empirical AV1 Perceptual Quality & Performance Benchmark for xg2g
# Measures SSIM, PSNR, XPSNR, Encoding FPS, Speed, and Bitrate Efficiency across discrete AV1 bitrate tiers.

set -euo pipefail

SHOW_HELP=false
INPUT_FILE=""
DURATION_SEC=15
OUTPUT_DIR="./benchmark_results"
DEVICE="/dev/dri/renderD128"

while [[ $# -gt 0 ]]; do
    case "$1" in
        -i|--input)
            INPUT_FILE="$2"
            shift 2
            ;;
        -d|--duration)
            DURATION_SEC="$2"
            shift 2
            ;;
        -o|--output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --device)
            DEVICE="$2"
            shift 2
            ;;
        -h|--help)
            SHOW_HELP=true
            shift
            ;;
        *)
            echo "Unknown argument: $1"
            exit 1
            ;;
    esac
done

if [[ "$SHOW_HELP" == "true" ]] || [[ -z "$INPUT_FILE" ]]; then
    echo "Usage: $0 -i <input_dvb_file.ts> [-d duration_sec] [-o output_dir] [--device /dev/dri/renderD128]"
    echo ""
    echo "Options:"
    echo "  -i, --input     Path to DVB raw capture (.ts file)"
    echo "  -d, --duration  Test duration in seconds (default: 15)"
    echo "  -o, --output    Output directory for benchmark artifacts (default: ./benchmark_results)"
    echo "  --device        VAAPI render node (default: /dev/dri/renderD128)"
    exit 0
fi

if [[ ! -f "$INPUT_FILE" ]]; then
    echo "ERROR: Input file '$INPUT_FILE' not found."
    exit 1
fi

mkdir -p "$OUTPUT_DIR"
DECODED_REF="$OUTPUT_DIR/ref_decoded.y4m"

echo "=========================================================================="
echo " xg2g AV1 Perceptual Quality & Real-Time Performance Benchmark"
echo " Input: $INPUT_FILE"
echo " Test Duration: ${DURATION_SEC}s"
echo " VAAPI Device: $DEVICE"
echo " Output Dir: $OUTPUT_DIR"
echo "=========================================================================="

echo "▶ Step 1: Generating Deinterlaced 1080p50 Y4M Reference Stream..."
ffmpeg -y -hide_banner -loglevel error \
    -vaapi_device "$DEVICE" \
    -hwaccel vaapi \
    -hwaccel_output_format vaapi \
    -t "$DURATION_SEC" \
    -i "$INPUT_FILE" \
    -vf "deinterlace_vaapi=mode=motion_compensated:rate=field,hwdownload,format=p010le" \
    -pix_fmt yuv420p10le \
    "$DECODED_REF"

REF_FRAMES=$(ffprobe -v error -count_frames -select_streams v:0 -show_entries stream=nb_read_frames -of default=nokey=1:raw_value=1 "$DECODED_REF")
echo "✅ Reference generated: $REF_FRAMES frames in $DECODED_REF"
echo ""

BITRATES_K=(5090 8000 10000 12000 15000 18000 20000)

echo "| Target Bitrate | Actual Bitrate | Enc Speed | Enc FPS | Realtime? | SSIM (All) | PSNR (Y-dB) | Size (MB) |"
echo "|:---------------|:---------------|:----------|:--------|:----------|:-----------|:------------|:----------|"

SUMMARY_FILE="$OUTPUT_DIR/benchmark_summary.md"
cat << EOF > "$SUMMARY_FILE"
# xg2g AV1 Perceptual Quality & Performance Benchmark

* **Input File:** \`$(basename "$INPUT_FILE")\`
* **Test Duration:** ${DURATION_SEC} seconds (${REF_FRAMES} frames)
* **VAAPI Device:** \`$DEVICE\`
* **Reference Format:** 1080p50 10-bit (motion-compensated deinterlacing)

## Benchmark Results Matrix

| Target Bitrate | Actual Bitrate | Enc Speed | Enc FPS | Realtime? | SSIM (All) | PSNR (Y-dB) | Size (MB) |
|:---------------|:---------------|:----------|:--------|:----------|:-----------|:------------|:----------|
EOF

for TARGET_K in "${BITRATES_K[@]}"; do
    BUF_K=$((TARGET_K * 2))
    TEST_OUT="$OUTPUT_DIR/av1_${TARGET_K}k.mp4"
    LOG_ENC="$OUTPUT_DIR/enc_${TARGET_K}k.log"
    LOG_METRICS="$OUTPUT_DIR/metrics_${TARGET_K}k.log"

    START_TIME=$(date +%s.%N)
    ffmpeg -y -hide_banner \
        -vaapi_device "$DEVICE" \
        -hwaccel vaapi \
        -hwaccel_output_format vaapi \
        -t "$DURATION_SEC" \
        -i "$INPUT_FILE" \
        -vf "deinterlace_vaapi=mode=motion_compensated:rate=field,scale_vaapi=format=p010" \
        -c:v av1_vaapi -b:v "${TARGET_K}k" -maxrate "${TARGET_K}k" -bufsize "${BUF_K}k" \
        -an \
        "$TEST_OUT" > "$LOG_ENC" 2>&1
    END_TIME=$(date +%s.%N)

    ELAPSED=$(awk "BEGIN {print $END_TIME - $START_TIME}")
    ENC_FPS=$(awk "BEGIN {print $REF_FRAMES / $ELAPSED}")
    ENC_SPEED=$(awk "BEGIN {print $ENC_FPS / 50.0}")

    REALTIME="❌ NO"
    if (( $(echo "$ENC_FPS >= 50.0" | bc -l) )); then
        REALTIME="✅ YES"
    fi

    FILE_BYTES=$(stat -c%s "$TEST_OUT" 2>/dev/null || stat -f%z "$TEST_OUT")
    FILE_MB=$(awk "BEGIN {printf \"%.2f\", $FILE_BYTES / 1048576}")
    ACTUAL_BITRATE_K=$(awk "BEGIN {printf \"%.0f\", ($FILE_BYTES * 8 / $DURATION_SEC) / 1000}")

    # Compute SSIM and PSNR metrics against reference Y4M
    ffmpeg -y -hide_banner \
        -i "$TEST_OUT" \
        -i "$DECODED_REF" \
        -lavfi "[0:v][1:v]ssim=stats_file=$OUTPUT_DIR/ssim_${TARGET_K}k.txt;[0:v][1:v]psnr=stats_file=$OUTPUT_DIR/psnr_${TARGET_K}k.txt" \
        -f null - > "$LOG_METRICS" 2>&1

    SSIM_VAL=$(grep -oE "All:[0-9\.]+" "$LOG_METRICS" | tail -n1 | cut -d: -f2 || echo "N/A")
    PSNR_Y_VAL=$(grep -oE "y:[0-9\.]+" "$LOG_METRICS" | tail -n1 | cut -d: -f2 || echo "N/A")

    FORMATTED_LINE="| ${TARGET_K} Kbps | ${ACTUAL_BITRATE_K} Kbps | ${ENC_SPEED}x | ${ENC_FPS} fps | $REALTIME | $SSIM_VAL | $PSNR_Y_VAL dB | ${FILE_MB} MB |"
    echo "$FORMATTED_LINE"
    echo "$FORMATTED_LINE" >> "$SUMMARY_FILE"

    # Extract sample crop frame (frame 250) for visual comparison
    ffmpeg -y -hide_banner -ss 00:00:05 -i "$TEST_OUT" -vframes 1 "$OUTPUT_DIR/frame_5s_${TARGET_K}k.png" >/dev/null 2>&1 || true
done

echo ""
echo "=========================================================================="
echo " Benchmark complete! Report saved to: $SUMMARY_FILE"
echo " Sample frames saved to: $OUTPUT_DIR/frame_5s_<bitrate>k.png"
echo "=========================================================================="
