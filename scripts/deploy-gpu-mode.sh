#!/bin/bash
# xg2g MODE 3 (GPU Transcoding) - Production Deployment Script
# Deploys GPU transcoding to production server with VAAPI support
# Usage: ./scripts/deploy-gpu-mode.sh [--rollback]

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
REMOTE_HOST="${REMOTE_HOST:-root@10.10.55.14}"
REMOTE_DIR="${REMOTE_DIR:-/root/xg2g}"
RECEIVER_IP="${RECEIVER_IP:-10.10.55.64}"
BOUQUET="${BOUQUET:-Favourites (TV)}"
API_PORT="${API_PORT:-18080}"
PROXY_PORT="${PROXY_PORT:-18000}"
GPU_PORT="${GPU_PORT:-8085}"
VAAPI_DEVICE="${VAAPI_DEVICE:-/dev/dri/renderD128}"

# Parse arguments
ROLLBACK=false
if [ "${1:-}" = "--rollback" ]; then
    ROLLBACK=true
fi

# Helper functions
print_header() {
    echo ""
    echo -e "${BLUE}==========================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}==========================================${NC}"
    echo ""
}

print_step() {
    echo -e "${YELLOW}[$1/$2]${NC} $3"
}

print_success() {
    echo -e "      ${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "      ${RED}✗${NC} $1"
}

print_info() {
    echo -e "      ${BLUE}→${NC} $1"
}

# Verify SSH connection
verify_ssh() {
    if ! ssh -o ConnectTimeout=5 "$REMOTE_HOST" "echo 'SSH OK'" > /dev/null 2>&1; then
        print_error "Cannot connect to $REMOTE_HOST"
        exit 1
    fi
}

# Check if GPU is available on remote
check_gpu() {
    print_step "1" "1" "Checking for GPU device on remote host..."

    if ssh "$REMOTE_HOST" "test -e $VAAPI_DEVICE" 2>/dev/null; then
        print_success "GPU device found: $VAAPI_DEVICE"

        # Try vainfo
        if ssh "$REMOTE_HOST" "command -v vainfo >/dev/null 2>&1" 2>/dev/null; then
            if ssh "$REMOTE_HOST" "vainfo" > /tmp/vainfo-check.log 2>&1; then
                print_success "VAAPI is functional"

                DRIVER=$(grep "Driver version" /tmp/vainfo-check.log | head -1 || echo "unknown")
                print_info "$DRIVER"
            else
                print_error "vainfo failed - GPU may not be configured properly"
                cat /tmp/vainfo-check.log
                exit 1
            fi
        else
            print_info "vainfo not installed (install libva-utils for testing)"
        fi
    else
        print_error "GPU device $VAAPI_DEVICE not found"
        echo ""
        echo "Available DRI devices:"
        ssh "$REMOTE_HOST" "ls -l /dev/dri/" 2>/dev/null || echo "  None found"
        exit 1
    fi
}

# =============================================================================
# Main Deployment
# =============================================================================

if [ "$ROLLBACK" = true ]; then
    print_header "MODE 3 Rollback to MODE 2 (Audio Proxy Only)"

    print_step "1" "2" "Stopping GPU-enabled container..."

    ssh "$REMOTE_HOST" "cd $REMOTE_DIR && docker compose -f docker-compose.gpu.yml down" 2>/dev/null || true
    print_success "GPU container stopped"

    print_step "2" "2" "Starting MODE 2 (Audio Proxy)..."

    ssh "$REMOTE_HOST" "cd $REMOTE_DIR && docker compose -f docker-compose.audio-proxy.yml up -d"
    print_success "Rolled back to MODE 2"

    echo ""
    print_success "Rollback complete"
    exit 0
fi

# Normal deployment
print_header "xg2g MODE 3 (GPU Transcoding) Deployment"

echo "Remote Host:    $REMOTE_HOST"
echo "Install Dir:    $REMOTE_DIR"
echo "Receiver:       $RECEIVER_IP"
echo "VAAPI Device:   $VAAPI_DEVICE"
echo "Proxy Port:     $PROXY_PORT"
echo "GPU Port:       $GPU_PORT"
echo "API Port:       $API_PORT"
echo ""

# Verify prerequisites
verify_ssh
check_gpu

# =============================================================================
# Phase 1: Update files
# =============================================================================
print_header "Phase 1: Update Deployment Files"

print_step "1" "3" "Copying docker-compose.gpu.yml to remote..."

if scp docker-compose.gpu.yml "$REMOTE_HOST:$REMOTE_DIR/" > /dev/null 2>&1; then
    print_success "Docker Compose file uploaded"
else
    print_error "Failed to upload Docker Compose file"
    exit 1
fi

print_step "2" "3" "Creating .env file for production..."

cat > /tmp/xg2g-gpu.env <<EOF
# xg2g MODE 3 (GPU Transcoding) Configuration
XG2G_OWI_BASE=http://$RECEIVER_IP
XG2G_OWI_USER=root
XG2G_OWI_PASS=\${XG2G_OWI_PASS:-yourpassword}
XG2G_BOUQUET=$BOUQUET

# GPU Transcoding
XG2G_ENABLE_GPU_TRANSCODING=true
XG2G_GPU_LISTEN=0.0.0.0:$GPU_PORT
XG2G_VAAPI_DEVICE=$VAAPI_DEVICE
XG2G_VIDEO_BITRATE=4M
XG2G_AUDIO_BITRATE=192k

# Stream Proxy
XG2G_ENABLE_STREAM_PROXY=true
XG2G_PROXY_LISTEN=:$PROXY_PORT

# Audio Transcoding (fallback)
XG2G_ENABLE_AUDIO_TRANSCODING=true
XG2G_USE_RUST_REMUXER=true
XG2G_AUDIO_CODEC=aac
XG2G_AUDIO_CHANNELS=2

# Optional
XG2G_EPG_ENABLED=true
XG2G_HDHR_ENABLED=false
EOF

if scp /tmp/xg2g-gpu.env "$REMOTE_HOST:$REMOTE_DIR/.env.gpu" > /dev/null 2>&1; then
    print_success ".env file created"
else
    print_error "Failed to upload .env file"
    exit 1
fi

print_step "3" "3" "Updating docker-compose.gpu.yml ports..."

# Update ports in docker-compose.gpu.yml on remote
ssh "$REMOTE_HOST" "cd $REMOTE_DIR && sed -i 's/\"8080:8080\"/\"$API_PORT:8080\"/' docker-compose.gpu.yml"
ssh "$REMOTE_HOST" "cd $REMOTE_DIR && sed -i 's/\"18000:18000\"/\"$PROXY_PORT:18000\"/' docker-compose.gpu.yml"
ssh "$REMOTE_HOST" "cd $REMOTE_DIR && sed -i 's/\"8085:8085\"/\"$GPU_PORT:8085\"/' docker-compose.gpu.yml"

print_success "Ports configured"

# =============================================================================
# Phase 2: Stop existing services
# =============================================================================
print_header "Phase 2: Stop Existing Services"

print_step "1" "2" "Stopping MODE 2 (Audio Proxy) if running..."

ssh "$REMOTE_HOST" "cd $REMOTE_DIR && docker compose -f docker-compose.audio-proxy.yml down" 2>/dev/null || true
print_success "MODE 2 stopped"

print_step "2" "2" "Freeing ports..."

ssh "$REMOTE_HOST" "
    for port in $API_PORT $PROXY_PORT $GPU_PORT; do
        PID=\$(lsof -ti:\$port 2>/dev/null || true)
        if [ -n \"\$PID\" ]; then
            kill -9 \$PID 2>/dev/null || true
        fi
    done
"

print_success "Ports freed"

# =============================================================================
# Phase 3: Pull latest image
# =============================================================================
print_header "Phase 3: Pull Latest Image"

print_step "1" "1" "Pulling ghcr.io/manugh/xg2g:latest..."

if ssh "$REMOTE_HOST" "docker pull ghcr.io/manugh/xg2g:latest" > /tmp/docker-pull.log 2>&1; then
    print_success "Latest image pulled"
else
    print_error "Failed to pull image"
    cat /tmp/docker-pull.log
    exit 1
fi

# =============================================================================
# Phase 4: Start MODE 3
# =============================================================================
print_header "Phase 4: Start GPU Transcoding Service"

print_step "1" "1" "Starting xg2g with GPU support..."

if ssh "$REMOTE_HOST" "cd $REMOTE_DIR && docker compose -f docker-compose.gpu.yml up -d" > /tmp/docker-up.log 2>&1; then
    print_success "Container started"
else
    print_error "Container start failed"
    cat /tmp/docker-up.log
    exit 1
fi

# =============================================================================
# Phase 5: Health checks
# =============================================================================
print_header "Phase 5: Health Checks"

print_step "1" "4" "Waiting for services to start..."

sleep 10

print_step "2" "4" "Checking API health..."

if ssh "$REMOTE_HOST" "curl -sf http://localhost:$API_PORT/api/v1/status" > /tmp/api-health.log 2>&1; then
    print_success "API is healthy"
    VERSION=$(cat /tmp/api-health.log | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || echo "unknown")
    print_info "Version: $VERSION"
else
    print_error "API health check failed"
    echo ""
    echo "Container logs:"
    ssh "$REMOTE_HOST" "docker logs xg2g-gpu-transcoding 2>&1 | tail -20"
    exit 1
fi

print_step "3" "4" "Checking GPU server health..."

if ssh "$REMOTE_HOST" "curl -sf http://localhost:$GPU_PORT/health" > /tmp/gpu-health.log 2>&1; then
    print_success "GPU server is responding"

    VAAPI_AVAILABLE=$(cat /tmp/gpu-health.log | grep -o '"vaapi_available":[^,}]*' | cut -d':' -f2 || echo "unknown")
    GPU_VERSION=$(cat /tmp/gpu-health.log | grep -o '"version":"[^"]*"' | cut -d'"' -f4 || echo "unknown")

    if [ "$VAAPI_AVAILABLE" = "true" ]; then
        print_success "VAAPI hardware acceleration enabled"
    else
        print_error "VAAPI not available - check GPU configuration"
    fi

    print_info "Transcoder version: $GPU_VERSION"
else
    print_error "GPU server health check failed"
    echo ""
    echo "Container logs:"
    ssh "$REMOTE_HOST" "docker logs xg2g-gpu-transcoding 2>&1 | tail -20"
    exit 1
fi

print_step "4" "4" "Checking container status..."

CONTAINER_STATUS=$(ssh "$REMOTE_HOST" "docker ps --filter name=xg2g-gpu-transcoding --format '{{.Status}}'" || echo "not found")

if echo "$CONTAINER_STATUS" | grep -q "Up"; then
    print_success "Container is running"
    print_info "$CONTAINER_STATUS"
else
    print_error "Container not running properly"
    echo ""
    echo "Container logs:"
    ssh "$REMOTE_HOST" "docker logs xg2g-gpu-transcoding 2>&1 | tail -30"
    exit 1
fi

# =============================================================================
# Summary
# =============================================================================
print_header "Deployment Complete"

echo -e "${GREEN}✓ MODE 3 (GPU Transcoding) successfully deployed!${NC}"
echo ""
echo "Access URLs:"
echo "  API:        http://$REMOTE_HOST:$API_PORT/api/v1/status"
echo "  Streams:    http://$REMOTE_HOST:$PROXY_PORT/SERVICE_REF"
echo "  GPU Health: http://$REMOTE_HOST:$GPU_PORT/health"
echo "  Metrics:    http://$REMOTE_HOST:$GPU_PORT/metrics"
echo ""
echo "Useful commands:"
echo "  View logs:     ssh $REMOTE_HOST 'docker logs -f xg2g-gpu-transcoding'"
echo "  View stats:    ssh $REMOTE_HOST 'docker stats xg2g-gpu-transcoding'"
echo "  Restart:       ssh $REMOTE_HOST 'cd $REMOTE_DIR && docker compose -f docker-compose.gpu.yml restart'"
echo "  Stop:          ssh $REMOTE_HOST 'cd $REMOTE_DIR && docker compose -f docker-compose.gpu.yml down'"
echo "  Rollback:      ./scripts/deploy-gpu-mode.sh --rollback"
echo ""

print_success "Deployment successful!"
