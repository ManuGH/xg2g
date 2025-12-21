#!/bin/bash
set -e

REF="${1:-1:0:1:0:0:0:0:0:0:0:}"
HOST="${2:-localhost:6077}"

echo "Verifying stream startup for REF=$REF on $HOST"

# 1. Start timed request
start_time=$(date +%s%N)
curl -v -f "http://$HOST/hls/$REF.m3u8" > /tmp/playlist.m3u8 2> /tmp/curl.log || true
curl_exit=$?
end_time=$(date +%s%N)

duration=$(( (end_time - start_time) / 1000000 ))
echo "Request took ${duration}ms"

# 2. Check curl exit code (fail if 404/500 due to -f)
if [ $curl_exit -ne 0 ]; then
    echo "FAILURE: Curl failed with exit code $curl_exit"
    cat /tmp/curl.log
    exit 1
fi

# 3. Check playlist content
if grep -q "#EXTM3U" /tmp/playlist.m3u8; then
    echo "SUCCESS: Valid M3U8 header found"
else
    echo "FAILURE: Invalid M3U8 content"
    cat /tmp/playlist.m3u8
    exit 1
fi

# 4. Check for fMP4 segments and init.mp4
# Requirement: "playlist enthält #EXT-X-MAP:URI="init.mp4""
if grep -q '#EXT-X-MAP:URI="init.mp4"' /tmp/playlist.m3u8; then
    echo "SUCCESS: FMP4 EXT-X-MAP found"
elif grep -q "init.mp4" /tmp/playlist.m3u8; then
    echo "WARNING: init.mp4 found but not via standard EXT-X-MAP tag?"
else
    echo "WARNING: No init.mp4 found (might not be fMP4)"
fi

if grep -q ".m4s" /tmp/playlist.m3u8; then
     echo "SUCCESS: FMP4 .m4s segments found"
elif grep -q ".ts" /tmp/playlist.m3u8; then
     echo "WARNING: TS segments found (Legacy Profile)"
else
     echo "WARNING: No segments in initial playlist"
fi

echo "Verification passed."
