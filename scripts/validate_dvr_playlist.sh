#!/bin/bash
# Safari DVR Playlist Validator
# cspell:ignore TARGETDURATION
# Validates HLS playlists for Safari DVR compliance

set -euo pipefail

PLAYLIST_URL="$1"

if [[ -z "$PLAYLIST_URL" ]]; then
    echo "Usage: $0 <playlist_url>"
    echo "Example: $0 http://localhost:8080/api/v3/sessions/abc123/hls/index.m3u8"
    exit 1
fi

echo "=== Safari DVR Playlist Validator ==="
echo "Fetching: $PLAYLIST_URL"
echo ""

CONTENT=$(curl -sS "$PLAYLIST_URL")

# Check 1: PLAYLIST-TYPE:EVENT
if echo "$CONTENT" | grep -q "EXT-X-PLAYLIST-TYPE:EVENT"; then
    echo "✓ PLAYLIST-TYPE:EVENT present"
else
    echo "✗ PLAYLIST-TYPE:EVENT missing"
fi

# Check 2: EXT-X-START
if echo "$CONTENT" | grep -q "EXT-X-START:TIME-OFFSET="; then
    OFFSET=$(echo "$CONTENT" | grep "EXT-X-START" | sed 's/.*TIME-OFFSET=\(-[0-9]*\).*/\1/')
    echo "✓ EXT-X-START present (offset: ${OFFSET}s)"
else
    echo "✗ EXT-X-START missing - Safari DVR scrubber will NOT appear"
fi

# Check 3: PROGRAM-DATE-TIME
if echo "$CONTENT" | grep -q "EXT-X-PROGRAM-DATE-TIME:"; then
    echo "✓ PROGRAM-DATE-TIME present"
else
    echo "✗ PROGRAM-DATE-TIME missing"
fi

# Check 4: Segment count
SEGMENTS=$(echo "$CONTENT" | grep -c "\.ts\|\.m4s")
echo "✓ Segment count: $SEGMENTS"

# Validate segment count for DVR
if [[ $SEGMENTS -ge 900 ]]; then
    echo "✓ Segment count sufficient for 30min+ DVR window"
else
    echo "⚠️  Segment count low (need ≥900 for 30min @ 2s segments)"
    echo "   Note: Segments accumulate over time. Wait 30+ minutes for full DVR window."
fi

# Check 5: Target Duration
TARGET_DUR=$(echo "$CONTENT" | grep "EXT-X-TARGETDURATION" | sed 's/.*:\([0-9]*\)/\1/' || echo "")
if [[ -n "$TARGET_DUR" ]]; then
    TOTAL_DURATION=$((SEGMENTS * TARGET_DUR))
    echo "✓ Estimated total duration: ${TOTAL_DURATION}s (~$((TOTAL_DURATION / 60))min)"
fi

echo ""
echo "=== Summary ==="
if echo "$CONTENT" | grep -q "EXT-X-START:TIME-OFFSET=" && [[ $SEGMENTS -ge 20 ]]; then
    echo "✅ Playlist is Safari DVR compatible"
else
    echo "❌ Playlist needs fixes for Safari DVR:"
    echo "$CONTENT" | grep -q "EXT-X-START:TIME-OFFSET=" || echo "   - Add EXT-X-START tag"
    [[ $SEGMENTS -ge 20 ]] || echo "   - Wait for more segments to accumulate"
fi
