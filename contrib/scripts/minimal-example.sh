#!/bin/bash
# Minimal xg2g Configuration Example
#
# This script demonstrates the absolute minimum configuration required
# to run xg2g with iOS Safari support.
#
# Audio transcoding and Rust remuxer are ENABLED BY DEFAULT (since Phase 6)
# You only need to configure:
#   1. Receiver IP and backend port
#   2. Rust library path

set -e

# Required configuration
RECEIVER_IP="${RECEIVER_IP:-192.168.1.100}"
BACKEND_PORT="${BACKEND_PORT:-17999}"
XG2G_DIR="${XG2G_DIR:-/root/xg2g}"

echo "=========================================="
echo "  Minimal xg2g Configuration"
echo "=========================================="
echo "RECEIVER_IP:  $RECEIVER_IP"
echo "BACKEND_PORT: $BACKEND_PORT"
echo "XG2G_DIR:     $XG2G_DIR"
echo "=========================================="
echo ""

# Change to xg2g directory
cd "$XG2G_DIR"

# Minimal configuration - audio transcoding enabled by default!
export LD_LIBRARY_PATH="$XG2G_DIR/transcoder/target/release"
export XG2G_OWI_BASE="http://$RECEIVER_IP:80"
export XG2G_PROXY_TARGET="http://$RECEIVER_IP:$BACKEND_PORT"
export XG2G_ENABLE_STREAM_PROXY=true

# Optional: Disable EPG/HDHR if not needed
export XG2G_EPG_ENABLED=false
export XG2G_HDHR_ENABLED=false

echo "Starting xg2g with iOS Safari support..."
echo ""
echo "Configuration:"
echo "  - Audio Transcoding: ENABLED (default)"
echo "  - Rust Remuxer:      ENABLED (default)"
echo "  - Codec:             AAC-LC (default)"
echo "  - Bitrate:           192k (default)"
echo "  - Channels:          2 (stereo, default)"
echo ""

# Start daemon
exec ./xg2g-daemon
