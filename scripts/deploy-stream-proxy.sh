#!/bin/bash
# Stream Proxy Deployment Script
#
# This script deploys the xg2g stream proxy with configurable backend routing.
#
# Usage:
#   ./deploy-stream-proxy.sh [BACKEND_PORT]
#
# Arguments:
#   BACKEND_PORT - Optional backend port (default: 8001)
#                  Common values: 8001 (direct), 17999 (alternative backend)
#
# Environment Variables:
#   RECEIVER_IP          - VU+ Enigma2 receiver IP address (required)
#   PROXY_LISTEN_PORT    - Public proxy listen port (default: 18000)
#   API_LISTEN_PORT      - API server listen port (default: 18080)
#   XG2G_INSTALL_DIR     - xg2g installation directory (default: /root/xg2g)
#   XG2G_BOUQUET         - Bouquet name (default: "Favourites (TV)")
#   RUST_LIB_PATH        - Rust library path (default: $XG2G_INSTALL_DIR/transcoder/target/release)

set -e

# Configuration defaults
BACKEND_PORT="${1:-8001}"
RECEIVER_IP="${RECEIVER_IP:-}"
PROXY_LISTEN_PORT="${PROXY_LISTEN_PORT:-18000}"
API_LISTEN_PORT="${API_LISTEN_PORT:-18080}"
XG2G_INSTALL_DIR="${XG2G_INSTALL_DIR:-/root/xg2g}"
XG2G_BOUQUET="${XG2G_BOUQUET:-Favourites (TV)}"
RUST_LIB_PATH="${RUST_LIB_PATH:-$XG2G_INSTALL_DIR/transcoder/target/release}"

# Validation
if [ -z "$RECEIVER_IP" ]; then
    echo "ERROR: RECEIVER_IP environment variable is required"
    echo "Usage: RECEIVER_IP=192.168.1.100 $0 [BACKEND_PORT]"
    exit 1
fi

if [ ! -d "$XG2G_INSTALL_DIR" ]; then
    echo "ERROR: xg2g installation directory not found: $XG2G_INSTALL_DIR"
    exit 1
fi

if [ ! -f "$XG2G_INSTALL_DIR/xg2g-daemon" ]; then
    echo "ERROR: xg2g-daemon binary not found: $XG2G_INSTALL_DIR/xg2g-daemon"
    exit 1
fi

if [ ! -d "$RUST_LIB_PATH" ]; then
    echo "ERROR: Rust library path not found: $RUST_LIB_PATH"
    exit 1
fi

echo "=========================================="
echo "  xg2g Stream Proxy Deployment"
echo "=========================================="
echo "Configuration:"
echo "  RECEIVER_IP:        $RECEIVER_IP"
echo "  BACKEND_PORT:       $BACKEND_PORT"
echo "  PROXY_LISTEN_PORT:  $PROXY_LISTEN_PORT"
echo "  API_LISTEN_PORT:    $API_LISTEN_PORT"
echo "  XG2G_INSTALL_DIR:   $XG2G_INSTALL_DIR"
echo "  RUST_LIB_PATH:      $RUST_LIB_PATH"
echo "  BOUQUET:            $XG2G_BOUQUET"
echo "=========================================="
echo ""

# Stop old processes
echo "[1/4] Stopping old processes..."
pkill -9 -f xg2g 2>/dev/null || true
pkill -9 socat 2>/dev/null || true
sleep 2
echo "      ✓ Old processes terminated"
echo ""

# Change to installation directory
cd "$XG2G_INSTALL_DIR"

# Export configuration
echo "[2/4] Configuring environment..."
export LD_LIBRARY_PATH="$RUST_LIB_PATH"
export XG2G_LISTEN=":$API_LISTEN_PORT"
export XG2G_OWI_BASE="http://$RECEIVER_IP:80"
export XG2G_XCPLUGIN_BASE="http://$RECEIVER_IP:80"
export XG2G_BOUQUET="$XG2G_BOUQUET"
export XG2G_EPG_ENABLED=false
export XG2G_HDHR_ENABLED=false

# Stream Proxy Configuration
export XG2G_ENABLE_STREAM_PROXY=true
export XG2G_PROXY_LISTEN=":$PROXY_LISTEN_PORT"
export XG2G_PROXY_TARGET="http://$RECEIVER_IP:$BACKEND_PORT"

# Audio Transcoding (enabled by default, explicitly set for clarity)
# Note: XG2G_ENABLE_AUDIO_TRANSCODING defaults to true
# Note: XG2G_USE_RUST_REMUXER defaults to true
export XG2G_ENABLE_AUDIO_TRANSCODING=true  # Explicit (default: true)
export XG2G_USE_RUST_REMUXER=true          # Explicit (default: true)
export XG2G_AUDIO_CODEC=aac                 # Explicit (default: aac)
export XG2G_AUDIO_BITRATE=192k              # Explicit (default: 192k)
export XG2G_AUDIO_CHANNELS=2                # Explicit (default: 2)
export RUST_LOG=debug

echo "      ✓ Environment configured"
echo "      → Proxy: $RECEIVER_IP:$PROXY_LISTEN_PORT → $RECEIVER_IP:$BACKEND_PORT"
echo ""

# Start daemon
echo "[3/4] Starting xg2g-daemon..."
./xg2g-daemon > /tmp/xg2g-stream-proxy.log 2>&1 &
DAEMON_PID=$!
sleep 3

# Verify daemon is running
if ! ps -p $DAEMON_PID > /dev/null 2>&1; then
    echo "      ✗ Failed to start xg2g-daemon"
    echo ""
    echo "Last 20 lines of log:"
    tail -20 /tmp/xg2g-stream-proxy.log
    exit 1
fi

echo "      ✓ xg2g-daemon started (PID: $DAEMON_PID)"
echo ""

# Health check
echo "[4/4] Running health checks..."
sleep 2

# Check if daemon is still running
if ! ps -p $DAEMON_PID > /dev/null 2>&1; then
    echo "      ✗ Daemon terminated unexpectedly"
    echo ""
    echo "Last 20 lines of log:"
    tail -20 /tmp/xg2g-stream-proxy.log
    exit 1
fi

# Check log for startup messages
if grep -q "starting stream proxy server" /tmp/xg2g-stream-proxy.log; then
    echo "      ✓ Stream proxy server started"
else
    echo "      ⚠ Stream proxy startup message not found in logs"
fi

if grep -q "API server listening" /tmp/xg2g-stream-proxy.log; then
    echo "      ✓ API server started"
else
    echo "      ⚠ API server startup message not found in logs"
fi

echo ""
echo "=========================================="
echo "  Deployment Complete!"
echo "=========================================="
echo ""
echo "Service Endpoints:"
echo "  Stream Proxy:  http://<THIS_SERVER_IP>:$PROXY_LISTEN_PORT/1:0:19:..."
echo "  API Server:    http://<THIS_SERVER_IP>:$API_LISTEN_PORT/api/..."
echo ""
echo "Backend Configuration:"
echo "  Target:        http://$RECEIVER_IP:$BACKEND_PORT"
echo "  Routing:       All channels → Backend Port $BACKEND_PORT"
echo ""
echo "Logs:"
echo "  tail -f /tmp/xg2g-stream-proxy.log"
echo ""
echo "Management:"
echo "  Stop:          pkill -9 xg2g"
echo "  Status:        ps aux | grep xg2g-daemon"
echo ""
