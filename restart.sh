#!/bin/bash
# Safe restart script for xg2g
# Kills only the Go daemon process, avoiding VS Code/SSH kills.

echo "Stopping xg2g..."
# Kill only the current user's daemon processes (no SIGKILL to avoid collateral damage)
CURRENT_UID="$(id -u)"
pkill -TERM -u "${CURRENT_UID}" -f "xg2g-daemon" 2>/dev/null || true
sleep 2

echo "Starting xg2g..."
set -a
[ -f .env ] && . .env
XG2G_LOG_LEVEL=debug
set +a
# Calculate version info (matching Makefile logic)
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT_HASH=$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "Debug: VERSION=${VERSION} COMMIT=${COMMIT_HASH}"

LDFLAGS="-s -w -buildid= -X main.version=${VERSION} -X main.commit=${COMMIT_HASH} -X main.buildDate=${BUILD_DATE}"

# Enable Rust Audio Remuxer & H.264 Repair
export XG2G_USE_RUST_REMUXER=true
export XG2G_H264_STREAM_REPAIR=true

# Enable Smart Video Transcoding (VAAPI Cascade: AV1->HEVC->H264)
export XG2G_VIDEO_TRANSCODE=true
export XG2G_VIDEO_CODEC=auto

# Audio Quality (High Fidelity)
export XG2G_AUDIO_BITRATE=192k

# Rate Limiting (Disabled for Home LAN usage)
export XG2G_RATELIMIT_ENABLED=false
# export XG2G_RATELIMIT_RPS=100
# export XG2G_RATELIMIT_BURST=200

echo "Cleaning build cache..."
go clean -cache
echo "Building xg2g version ${VERSION}..."
go build -v -ldflags "${LDFLAGS}" -o xg2g-daemon ./cmd/daemon

echo "Starting Port Heist..."
# Port Heist Strategy: Kill until dead or we succeed
# Iterate to ensure the port is freed
echo "Attempting to reclaim port (Port Heist)..."
for i in {1..5}; do
    # Safe Kill: Only kill processes owned by current user
    pkill -u "$(id -u)" -f "xg2g" || true
    sleep 1
    
    # Check if port is free (assuming 8080/default)
    if ! ss -lptn | grep -q ":8080 "; then
         break
    fi
    echo "Port still busy, retrying kill..."
done
echo "Port cleared."

# Start daemon
mkdir -p logs
./xg2g-daemon > logs/dev_output.log 2>&1 &
PID=$!
echo "Started xg2g with PID $PID"

echo "Done. Logs: tail -f logs/dev_output.log"
