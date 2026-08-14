#!/usr/bin/env bash
# benchmark_av1_quality.sh
# Empirical AV1 Perceptual Quality & Real-Time Performance Benchmark Suite for xg2g
# Measures SSIM, PSNR, XPSNR, Encoding FPS, Realtime Headroom, and Bitrate Efficiency across discrete AV1 bitrate tiers.

set -euo pipefail

SHOW_HELP=false
INPUT_FILE=""
CATEGORY="GENERAL"
DURATION_SEC=30
OUTPUT_DIR="./benchmark_results"
DEVICE="/dev/dri/renderD128"

while [[ $# -gt 0 ]]; do
    case "$1" in
        -i|--input)
            INPUT_FILE="$2"
            shift 2
            ;;
        -c|--category)
            CATEGORY="$2"
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
    echo "Usage: $0 -i <input_dvb_file.ts> [-c category] [-d duration_sec] [-o output_dir] [--device /dev/dri/renderD128]"
    echo ""
    echo "Options:"
    echo "  -i, --input     Path to DVB raw capture (.ts file)"
    echo "  -c, --category  Scene Category: SPORT | HIGH_MOTION | GRAIN | DARK | CLEAN_STUDIO (default: GENERAL)"
    echo "  -d, --duration  Test duration in seconds (default: 30)"
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
echo " xg2g Empirical AV1 Quality & Real-Time Headroom Benchmark"
echo " Input: $INPUT_FILE"
echo " Scene Category: $CATEGORY"
echo " Test Duration: ${DURATION_SEC}s"
echo " VAAPI Device: $DEVICE"
echo " Output Dir: $OUTPUT_DIR"
echo "=========================================================================="

echo "▶ Step 1: Generating Deinterlaced 1080p50 Reference Y4M Stream..."
echo "  Pipeline: deinterlace_vaapi=mode=motion_compensated:rate=field,scale_vaapi=format=p010,hwdownload"

ffmpeg -y -hide_banner -loglevel error \
    -vaapi_device "$DEVICE" \
    -hwaccel vaapi \
    -hwaccel_output_format vaapi \
    -t "$DURATION_SEC" \
    -i "$INPUT_FILE" \
    -vf "deinterlace_vaapi=mode=motion_compensated:rate=field,scale_vaapi=format=p010,hwdownload,format=p010le" \
    -pix_fmt yuv420p10le \
    -strict -1 \
    "$DECODED_REF"

REF_FRAMES=$(ffprobe -v error -count_frames -select_streams v:0 -show_entries stream=nb_read_frames -of default=noprint_wrappers=1:nokey=1 "$DECODED_REF")
REF_WIDTH=$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of default=noprint_wrappers=1:nokey=1 "$DECODED_REF")
REF_HEIGHT=$(ffprobe -v error -select_streams v:0 -show_entries stream=height -of default=noprint_wrappers=1:nokey=1 "$DECODED_REF")

echo "✅ Reference generated: $REF_FRAMES frames ($REF_WIDTH x $REF_HEIGHT @ 50fps) in $DECODED_REF"
echo ""

BITRATES_K=(5090 8000 10000 12000 15000 18000 20000)

SUMMARY_FILE="$OUTPUT_DIR/benchmark_summary_${CATEGORY}.md"
cat << EOF > "$SUMMARY_FILE"
# xg2g AV1 Empirical Quality & Performance Benchmark

* **Input File:** \`$(basename "$INPUT_FILE")\`
* **Scene Category:** \`$CATEGORY\`
* **Test Duration:** ${DURATION_SEC} seconds (${REF_FRAMES} frames)
* **VAAPI Device:** \`$DEVICE\`
* **Reference Format:** 1080p50 10-bit Y4M (\`deinterlace_vaapi=motion_compensated\` + \`scale_vaapi=format=p010\`)

## Production Headroom Legend
* **🚀 OPTIMAL:** \`>= 60 fps\` (Speed \`>= 1.20x\` - 20%+ production headroom)
* **✅ GOOD:** \`55.0 - 59.9 fps\` (Speed \`1.10x - 1.19x\` - 10%+ production headroom)
* **⚠️ MARGINAL:** \`50.0 - 54.9 fps\` (Speed \`1.00x - 1.09x\` - No headroom reserve)
* **❌ FAIL:** \`< 50.0 fps\` (Speed \`< 1.00x\` - Under-realtime, dropped frames)

## Benchmark Results Matrix

| Target | Actual Bitrate | Enc Speed | Enc FPS | Production Realtime? | SSIM (All) | PSNR (Y-dB) | XPSNR (Y-dB) | Size (MB) |
|:-------|:---------------|:----------|:--------|:---------------------|:-----------|:------------|:-------------|:----------|
EOF

echo "| Target | Actual Bitrate | Enc Speed | Enc FPS | Production Realtime? | SSIM (All) | PSNR (Y-dB) | XPSNR (Y-dB) | Size (MB) |"
echo "|:-------|:---------------|:----------|:--------|:---------------------|:-----------|:------------|:-------------|:----------|"

for TARGET_K in "${BITRATES_K[@]}"; do
    BUF_K=$((TARGET_K * 2))
    TEST_OUT="$OUTPUT_DIR/av1_${TARGET_K}k.mp4"
    LOG_ENC="$OUTPUT_DIR/enc_${TARGET_K}k.log"
    LOG_SSIM="$OUTPUT_DIR/ssim_${TARGET_K}k.log"
    LOG_PSNR="$OUTPUT_DIR/psnr_${TARGET_K}k.log"
    LOG_XPSNR="$OUTPUT_DIR/xpsnr_${TARGET_K}k.log"

    # Multi-Run Warmup & 3-Pass Median Performance Sampling
    RUN_TIMES=()
    for RUN_IDX in 1 2 3; do
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
        RUN_ELAPSED=$(awk "BEGIN {print $END_TIME - $START_TIME}")
        RUN_TIMES+=("$RUN_ELAPSED")
    done

    # Sort run times to pick median (index 1 of 3)
    MEDIAN_ELAPSED=$(printf '%s\n' "${RUN_TIMES[@]}" | sort -n | sed -n '2p')
    ENC_FPS=$(awk "BEGIN {print $REF_FRAMES / $MEDIAN_ELAPSED}")
    ENC_SPEED=$(awk "BEGIN {printf \"%.2f\", $ENC_FPS / 50.0}")
    FORMATTED_FPS=$(awk "BEGIN {printf \"%.1f\", $ENC_FPS}")

    # Production Headroom Assessment
    HEADROOM_STATUS=$(awk "BEGIN {
        if ($ENC_FPS >= 60.0) print \"🚀 OPTIMAL\"
        else if ($ENC_FPS >= 55.0) print \"✅ GOOD\"
        else if ($ENC_FPS >= 50.0) print \"⚠️ MARGINAL\"
        else print \"❌ FAIL\"
    }")

    # Pre-Validation: Verify exact frame count and resolution match before computing metrics
    TEST_FRAMES=$(ffprobe -v error -count_frames -select_streams v:0 -show_entries stream=nb_read_frames -of default=noprint_wrappers=1:nokey=1 "$TEST_OUT")
    TEST_WIDTH=$(ffprobe -v error -select_streams v:0 -show_entries stream=width -of default=noprint_wrappers=1:nokey=1 "$TEST_OUT")
    TEST_HEIGHT=$(ffprobe -v error -select_streams v:0 -show_entries stream=height -of default=noprint_wrappers=1:nokey=1 "$TEST_OUT")

    if [[ "$TEST_FRAMES" != "$REF_FRAMES" ]] || [[ "$TEST_WIDTH" != "$REF_WIDTH" ]] || [[ "$TEST_HEIGHT" != "$REF_HEIGHT" ]]; then
        echo "ERROR: Frame alignment or dimension mismatch for ${TARGET_K}k! Ref: ${REF_FRAMES}f (${REF_WIDTH}x${REF_HEIGHT}), Enc: ${TEST_FRAMES}f (${TEST_WIDTH}x${TEST_HEIGHT})" >&2
        exit 1
    fi

    FILE_BYTES=$(stat -c%s "$TEST_OUT" 2>/dev/null || stat -f%z "$TEST_OUT")
    FILE_MB=$(awk "BEGIN {printf \"%.2f\", $FILE_BYTES / 1048576}")
    ACTUAL_BITRATE_K=$(awk "BEGIN {printf \"%.0f\", ($FILE_BYTES * 8 / $DURATION_SEC) / 1000}")

    # Robust Separate Filter Runs: SSIM, PSNR, and XPSNR
    ffmpeg -y -hide_banner -i "$TEST_OUT" -i "$DECODED_REF" -lavfi "[0:v][1:v]ssim=stats_file=$OUTPUT_DIR/ssim_${TARGET_K}k.txt" -f null - > "$LOG_SSIM" 2>&1
    ffmpeg -y -hide_banner -i "$TEST_OUT" -i "$DECODED_REF" -lavfi "[0:v][1:v]psnr=stats_file=$OUTPUT_DIR/psnr_${TARGET_K}k.txt" -f null - > "$LOG_PSNR" 2>&1
    ffmpeg -y -hide_banner -i "$TEST_OUT" -i "$DECODED_REF" -lavfi "[0:v][1:v]xpsnr=stats_file=$OUTPUT_DIR/xpsnr_${TARGET_K}k.txt" -f null - > "$LOG_XPSNR" 2>&1

    SSIM_VAL=$(grep -oE "All:[0-9\.]+" "$LOG_SSIM" | tail -n1 | cut -d: -f2 || echo "N/A")
    PSNR_Y_VAL=$(grep -oE "y:[0-9\.]+" "$LOG_PSNR" | tail -n1 | cut -d: -f2 || echo "N/A")
    XPSNR_Y_VAL=$(grep -oE "y:[0-9\.]+" "$LOG_XPSNR" | tail -n1 | cut -d: -f2 || echo "N/A")

    FORMATTED_LINE="| ${TARGET_K} Kbps | ${ACTUAL_BITRATE_K} Kbps | ${ENC_SPEED}x | ${FORMATTED_FPS} fps | $HEADROOM_STATUS | $SSIM_VAL | $PSNR_Y_VAL dB | $XPSNR_Y_VAL dB | ${FILE_MB} MB |"
    echo "$FORMATTED_LINE"
    echo "$FORMATTED_LINE" >> "$SUMMARY_FILE"

    # Multi-Frame Full Frame Extractions at 2s, 5s, 10s, 15s, 20s, 25s for visual inspection
    for TIMESTAMP_SEC in 2 5 10 15 20 25; do
        if (( TIMESTAMP_SEC < DURATION_SEC )); then
            ffmpeg -y -hide_banner -ss "$TIMESTAMP_SEC" -i "$TEST_OUT" -vframes 1 "$OUTPUT_DIR/frame_${TIMESTAMP_SEC}s_${TARGET_K}k.png" >/dev/null 2>&1 || true
        fi
    done
done

echo ""
echo "=========================================================================="
echo " Benchmark complete! Markdown report saved to: $SUMMARY_FILE"
echo " Multi-frame PNG snapshots saved to: $OUTPUT_DIR/frame_<timestamp>s_<bitrate>k.png"
echo "=========================================================================="
