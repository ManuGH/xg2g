#!/bin/bash
# Safe restart script for xg2g
# Kills only the Go daemon process, avoiding VS Code/SSH kills.

echo "Stopping xg2g..."
# Kill by exact binary name
pgrep -f "xg2g-daemon" | xargs -r kill -9
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

LDFLAGS="-s -w -buildid= -X github.com/ManuGH/xg2g/cmd/daemon.version=${VERSION} -X github.com/ManuGH/xg2g/cmd/daemon.commit=${COMMIT_HASH} -X github.com/ManuGH/xg2g/cmd/daemon.buildDate=${BUILD_DATE}"

# Enable Rust Audio Remuxer & H.264 Repair
export XG2G_USE_RUST_REMUXER=true
export XG2G_H264_STREAM_REPAIR=true

# Enable Smart Video Transcoding (VAAPI Cascade: AV1->HEVC->H264)
export XG2G_VIDEO_TRANSCODE=true
export XG2G_VIDEO_CODEC=auto

# Audio Quality (High Fidelity)
export XG2G_AUDIO_BITRATE=320k

# Rate Limiting (Increased for Picons)
export XG2G_RATELIMIT_RPS=100
export XG2G_RATELIMIT_BURST=200

echo "Cleaning build cache..."
go clean -cache
echo "Building xg2g version ${VERSION}..."
go build -v -ldflags "${LDFLAGS}" -o xg2g-daemon ./cmd/daemon
nohup ./xg2g-daemon > dev_output.log 2>&1 &

echo "Done. Logs: tail -f dev_output.log"
