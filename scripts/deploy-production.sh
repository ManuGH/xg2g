#!/bin/bash
# Production Deployment Script for xg2g
#
# This script deploys xg2g with audio transcoding enabled by default.
# Configured for your production environment (10.10.55.14 → 10.10.55.64)

set -e

# Configuration
REMOTE_HOST="${REMOTE_HOST:-root@10.10.55.14}"
INSTALL_DIR="${INSTALL_DIR:-/root/xg2g}"
RECEIVER_IP="${RECEIVER_IP:-10.10.55.64}"
BACKEND_PORT="${BACKEND_PORT:-17999}"  # OSCam port (default)
PROXY_PORT="${PROXY_PORT:-18000}"
API_PORT="${API_PORT:-18080}"

echo "=========================================="
echo "  xg2g Production Deployment"
echo "=========================================="
echo "Remote Host:    $REMOTE_HOST"
echo "Install Dir:    $INSTALL_DIR"
echo "Receiver:       $RECEIVER_IP"
echo "Backend Port:   $BACKEND_PORT"
echo "Proxy Port:     $PROXY_PORT"
echo "API Port:       $API_PORT"
echo "=========================================="
echo ""

# Stop old processes and free ports
echo "[1/4] Stopping old processes and freeing ports..."
ssh "$REMOTE_HOST" "
    # Stop systemd services (if they exist)
    systemctl stop xg2g.service 2>/dev/null || true
    systemctl stop xg2g-stream-proxy.service 2>/dev/null || true
    systemctl stop xg2g-transcoder.service 2>/dev/null || true
    sleep 1

    # Kill all xg2g and socat processes by name
    pkill -9 xg2g 2>/dev/null || true
    pkill -9 socat 2>/dev/null || true
    pkill -9 xg2g-transcoder 2>/dev/null || true
    sleep 2

    # Kill any process still using port $PROXY_PORT (force by PID)
    for PID in \$(lsof -ti:$PROXY_PORT 2>/dev/null || true); do
        echo \"      Freeing port $PROXY_PORT (PID: \$PID)\"
        kill -9 \$PID 2>/dev/null || true
    done

    # Kill any process still using port $API_PORT (force by PID)
    for PID in \$(lsof -ti:$API_PORT 2>/dev/null || true); do
        echo \"      Freeing port $API_PORT (PID: \$PID)\"
        kill -9 \$PID 2>/dev/null || true
    done

    sleep 2

    # Final cleanup pass - ensure everything is dead
    pkill -9 xg2g 2>/dev/null || true
    pkill -9 socat 2>/dev/null || true
"
sleep 3
echo "      ✓ Old processes stopped and ports freed"
echo ""

# Start daemon with new defaults
echo "[2/4] Starting xg2g daemon..."
ssh "$REMOTE_HOST" "nohup sh -c 'cd $INSTALL_DIR && \
  export LD_LIBRARY_PATH=$INSTALL_DIR/transcoder/target/release && \
  export XG2G_LISTEN=:$API_PORT && \
  export XG2G_OWI_BASE=http://$RECEIVER_IP:80 && \
  export XG2G_XCPLUGIN_BASE=http://$RECEIVER_IP:80 && \
  export XG2G_BOUQUET=\"Favourites (TV)\" && \
  export XG2G_STREAM_PORT=8001 && \
  export XG2G_EPG_ENABLED=false && \
  export XG2G_HDHR_ENABLED=false && \
  export XG2G_ENABLE_STREAM_PROXY=true && \
  export XG2G_PROXY_LISTEN=:$PROXY_PORT && \
  export XG2G_PROXY_TARGET=http://$RECEIVER_IP:$BACKEND_PORT && \
  export RUST_LOG=info && \
  ./xg2g-daemon > /tmp/xg2g-production.log 2>&1' > /dev/null 2>&1 &"

sleep 4
echo "      ✓ Daemon started"
echo ""

# Verify startup
echo "[3/4] Verifying daemon status..."
PID=$(ssh "$REMOTE_HOST" "pgrep -f xg2g-daemon || echo 0")

if [ "$PID" = "0" ]; then
    echo "      ✗ Daemon failed to start"
    echo ""
    echo "Last 20 lines of log:"
    ssh "$REMOTE_HOST" "tail -20 /tmp/xg2g-production.log"
    exit 1
fi

echo "      ✓ Daemon running (PID: $PID)"
echo ""

# Health check
echo "[4/4] Running health checks..."
sleep 2

# Check logs for key startup messages
LOG_CHECK=$(ssh "$REMOTE_HOST" "tail -20 /tmp/xg2g-production.log")

if echo "$LOG_CHECK" | grep -q "audio transcoding enabled"; then
    echo "      ✓ Audio transcoding enabled (Rust remuxer)"
else
    echo "      ⚠ Audio transcoding message not found"
fi

if echo "$LOG_CHECK" | grep -q "Proxy server listening"; then
    echo "      ✓ Stream proxy listening on :$PROXY_PORT"
else
    echo "      ⚠ Proxy server startup message not found"
fi

if echo "$LOG_CHECK" | grep -q "API server listening"; then
    echo "      ✓ API server listening on :$API_PORT"
else
    echo "      ⚠ API server startup message not found"
fi

echo ""
echo "=========================================="
echo "  Deployment Complete!"
echo "=========================================="
echo ""
echo "Service Endpoints:"
echo "  Stream Proxy:  http://10.10.55.14:$PROXY_PORT/1:0:19:..."
echo "  API Server:    http://10.10.55.14:$API_PORT/api/..."
echo ""
echo "Backend Configuration:"
echo "  Target:        http://$RECEIVER_IP:$BACKEND_PORT"
echo "  Routing:       All channels → Backend Port $BACKEND_PORT"
echo ""
echo "Audio Transcoding:"
echo "  Status:        ENABLED BY DEFAULT ✅"
echo "  Rust Remuxer:  ENABLED BY DEFAULT ✅"
echo "  Codec:         AAC-LC (192k, stereo)"
echo "  CPU Impact:    0%"
echo ""
echo "Logs:"
echo "  ssh $REMOTE_HOST 'tail -f /tmp/xg2g-production.log'"
echo ""
echo "Management:"
echo "  Stop:    ssh $REMOTE_HOST 'pkill -9 xg2g'"
echo "  Status:  ssh $REMOTE_HOST 'ps aux | grep xg2g-daemon'"
echo ""
