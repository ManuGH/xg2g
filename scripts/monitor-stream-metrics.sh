#!/usr/bin/env bash
# Stream Monitoring Script - Validates Best Practice 2026 HLS Configuration
set -euo pipefail

echo "=== xg2g Stream Metrics Monitor ==="
echo "Watching for: FFmpeg parameters, Playlist Ready timing, R_PACKAGER_FAILED errors"
echo ""
echo "Waiting for stream start..."
echo ""

# Timestamps for metric calculation
declare -A stream_start_times
declare -A ready_times

# Process journalctl output line by line
journalctl -u xg2g -f --no-pager -o json | while read -r line; do
    msg=$(echo "$line" | jq -r '.MESSAGE // empty')
    ts=$(echo "$line" | jq -r '.__REALTIME_TIMESTAMP // empty')

    # Convert microsecond timestamp to seconds
    if [[ -n "$ts" ]]; then
        ts_sec=$((ts / 1000000))
    fi

    # 1) FFmpeg Invocation - Extract HLS parameters
    if echo "$msg" | grep -q "started media process"; then
        session_id=$(echo "$msg" | jq -r '.spec_id // empty' 2>/dev/null || echo "unknown")
        pid=$(echo "$msg" | jq -r '.pid // empty' 2>/dev/null || echo "unknown")
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ✓ FFmpeg started: PID=$pid Session=$session_id"
        stream_start_times["$session_id"]=$ts_sec
    fi

    # Extract actual FFmpeg command line if logged
    if echo "$msg" | grep -qE "hls_time|hls_list_size|hls_flags"; then
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] 📋 HLS Parameters detected:"
        echo "$msg" | grep -oE '\-hls_[a-z_]+ [0-9a-z_+]+' | while read -r param; do
            echo "   $param"
        done
    fi

    # 2) Playlist Ready Check
    if echo "$msg" | grep -q "checkPlaylistReady"; then
        session_id=$(echo "$msg" | jq -r '.session_id // .spec_id // empty' 2>/dev/null || echo "unknown")
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] 🔍 Playlist check: Session=$session_id"

        # Check for success
        if echo "$msg" | grep -qi "ready"; then
            ready_times["$session_id"]=$ts_sec
            if [[ -n "${stream_start_times[$session_id]:-}" ]]; then
                duration=$((ts_sec - stream_start_times[$session_id]))
                echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ✅ READY after ${duration}s (Target: >12s optimal)"
            else
                echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ✅ READY"
            fi
        fi
    fi

    # 3) Session Ready Status
    if echo "$msg" | jq -e '.status == "ready"' >/dev/null 2>&1; then
        session_id=$(echo "$msg" | jq -r '.session_id // empty' 2>/dev/null || echo "unknown")
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] 🎉 Session READY: $session_id"

        if [[ -n "${stream_start_times[$session_id]:-}" ]]; then
            duration=$((ts_sec - stream_start_times[$session_id]))
            echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ⏱  Time to READY: ${duration}s"
        fi
    fi

    # 4) R_PACKAGER_FAILED Errors
    if echo "$msg" | grep -q "R_PACKAGER_FAILED"; then
        session_id=$(echo "$msg" | jq -r '.session_id // empty' 2>/dev/null || echo "unknown")
        reason=$(echo "$msg" | jq -r '.detail // .reason // empty' 2>/dev/null || echo "unknown")
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ❌ R_PACKAGER_FAILED: Session=$session_id Reason=$reason"

        if [[ -n "${stream_start_times[$session_id]:-}" ]]; then
            duration=$((ts_sec - stream_start_times[$session_id]))
            echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ⏱  Failed after ${duration}s"
        fi
    fi

    # 5) Playlist not ready timeout
    if echo "$msg" | grep -qi "playlist not ready"; then
        session_id=$(echo "$msg" | jq -r '.session_id // empty' 2>/dev/null || echo "unknown")
        echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ⚠️  Playlist not ready timeout: Session=$session_id"

        if [[ -n "${stream_start_times[$session_id]:-}" ]]; then
            duration=$((ts_sec - stream_start_times[$session_id]))
            echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ⏱  Timeout after ${duration}s (Race condition?)"
        fi
    fi

    # 6) HLS Segment creation
    if echo "$msg" | grep -qE "stream[0-9]+\.ts"; then
        segment=$(echo "$msg" | grep -oE 'stream[0-9]+\.ts' | head -1)
        segment_num=$(echo "$segment" | grep -oE '[0-9]+')
        session_id=$(echo "$msg" | jq -r '.session_id // empty' 2>/dev/null || echo "unknown")

        # Only log every 5th segment to avoid spam
        if (( segment_num % 5 == 0 )); then
            echo "[$(date -d @${ts_sec} '+%H:%M:%S')] 📦 Segment $segment_num created"
        fi
    fi

    # 7) FFmpeg errors
    if echo "$msg" | grep -qiE "decode_slice_header|invalid data|error"; then
        if echo "$msg" | grep -qE "ffmpeg|h264"; then
            # Only show first 100 chars to avoid spam
            short_msg=$(echo "$msg" | cut -c1-100)
            echo "[$(date -d @${ts_sec} '+%H:%M:%S')] ⚠️  FFmpeg: ${short_msg}..."
        fi
    fi
done
