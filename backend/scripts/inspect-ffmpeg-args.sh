#!/usr/bin/env bash
# Inspects running FFmpeg processes to validate HLS parameters
set -euo pipefail

echo "=== FFmpeg Process Inspector ==="
echo ""

# Find all FFmpeg processes spawned by xg2g
ffmpeg_pids=$(pgrep -f "ffmpeg.*index.m3u8" || true)

if [[ -z "$ffmpeg_pids" ]]; then
    echo "❌ No active FFmpeg processes found"
    exit 0
fi

for pid in $ffmpeg_pids; do
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo "📹 FFmpeg PID: $pid"
    echo ""

    # Get full command line
    cmdline=$(cat /proc/$pid/cmdline | tr '\0' ' ')

    # Check for HLS parameters
    echo "🔍 HLS Configuration:"
    echo ""

    hls_time="unknown"
    if echo "$cmdline" | grep -q -- "-hls_time"; then
        hls_time=$(echo "$cmdline" | grep -oP '\-hls_time \K[0-9]+' || echo "NOT FOUND")
        if [[ "$hls_time" == "6" ]]; then
            echo "   ✅ hls_time: $hls_time (Standard Profile - Best Practice 2026)"
        elif [[ "$hls_time" == "1" ]]; then
            echo "   ✅ hls_time: $hls_time (Low Latency Profile)"
        else
            echo "   ⚠️  hls_time: $hls_time (Expected: 6 or 1)"
        fi
    else
        echo "   ❌ hls_time: NOT SET (using ffmpeg default: 2)"
    fi

    if echo "$cmdline" | grep -q -- "-hls_list_size"; then
        hls_list_size=$(echo "$cmdline" | grep -oP '\-hls_list_size \K[0-9]+' || echo "NOT FOUND")
        if [[ "$hls_list_size" == "10" && "$hls_time" == "6" ]]; then
            window_sec=$((hls_list_size * 6))
            echo "   ✅ hls_list_size: $hls_list_size (${window_sec}s window - Standard Profile)"
        elif [[ "$hls_time" == "1" ]]; then
             window_sec=$((hls_list_size * 1))
             echo "   ℹ️  hls_list_size: $hls_list_size (${window_sec}s window - Low Latency)"
        else
            echo "   ℹ️  hls_list_size: $hls_list_size"
        fi
    else
        echo "   ❌ hls_list_size: NOT SET (using ffmpeg default: 5)"
    fi

    if echo "$cmdline" | grep -q -- "-hls_flags"; then
        hls_flags=$(echo "$cmdline" | grep -oP '\-hls_flags \K[a-z_+]+' || echo "NOT FOUND")
        if echo "$hls_flags" | grep -q "delete_segments"; then
            echo "   ✅ hls_flags: $hls_flags (delete_segments enabled)"
        else
            echo "   ⚠️  hls_flags: $hls_flags (Missing delete_segments)"
        fi

        if echo "$hls_flags" | grep -q "append_list"; then
            echo "   ✅ hls_flags: append_list enabled"
        else
            echo "   ⚠️  hls_flags: Missing append_list"
        fi
    else
        echo "   ❌ hls_flags: NOT SET"
    fi

    echo ""
    echo "🔍 Encoding Configuration:"

    # Check Audio
    audio_ch=$(echo "$cmdline" | grep -oP '\-ac \K[0-9]+' || echo "unknown")
    if [[ "$audio_ch" == "2" ]]; then
       echo "   ✅ Audio: Stereo (Universal Policy)"
    elif [[ "$audio_ch" == "6" ]]; then
       echo "   ⚠️  Audio: 5.1 Surround (Legacy/High-Bandwidth)"
    else
       echo "   ℹ️  Audio: $audio_ch channels"
    fi

    # Check GOP/Keyint
    if echo "$cmdline" | grep -q "scenecut=0"; then
       echo "   ✅ GOP: Fixed (scenecut=0 active)"
    else
       echo "   ⚠️  GOP: Potential variability (scenecut=0 missing)"
    fi

    echo ""
    echo "📊 Process Info:"
    runtime=$(ps -p "$pid" -o etime= | xargs)
    echo "   Runtime: $runtime"

    cpu=$(ps -p "$pid" -o %cpu= | xargs)
    mem=$(ps -p "$pid" -o %mem= | xargs)
    echo "   CPU: ${cpu}% | Memory: ${mem}%"

    echo ""
    echo "📁 Output Directory:"
    session_dir=$(echo "$cmdline" | grep -oP '/[^ ]+/index\.m3u8' | xargs dirname || echo "unknown")
    echo "   $session_dir"

    if [[ -d "$session_dir" ]]; then
        segment_count=$(find "$session_dir" -name "seg_*.ts" 2>/dev/null | wc -l)
        echo "   Segments: $segment_count"

        if [[ -f "$session_dir/index.m3u8" ]]; then
            playlist_segments=$(grep -c "seg_[0-9]*.ts" "$session_dir/index.m3u8" || echo "0")
            echo "   Playlist entries: $playlist_segments"

            if [[ "$playlist_segments" -gt 0 ]]; then
                first_segment=$(grep -oE "seg_[0-9]+\.ts" "$session_dir/index.m3u8" | head -1 || echo "none")
                last_segment=$(grep -oE "seg_[0-9]+\.ts" "$session_dir/index.m3u8" | tail -1 || echo "none")
                echo "   Window: $first_segment → $last_segment"
            fi
        fi
    fi

    echo ""
done

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "💡 Summary:"
echo "   Standard Profile (Best Practice 2026):"
echo "   • hls_time: 6 (6s segments)"
echo "   • hls_list_size: 10 (60s window)"
echo ""
echo "   Low Latency Profile:"
echo "   • hls_time: 1 (1s segments)"
echo "   • hls_flags: delete_segments+append_list"
echo ""
