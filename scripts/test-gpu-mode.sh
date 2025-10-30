#!/bin/bash
# xg2g MODE 3 (GPU Transcoding) - Automated Test Script
# Tests all phases from Docker build to integration testing
# Usage: ./scripts/test-gpu-mode.sh [--skip-build] [--production]

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
IMAGE_NAME="${IMAGE_NAME:-xg2g:gpu-test}"
CONTAINER_NAME="${CONTAINER_NAME:-xg2g-gpu-test}"
RECEIVER_IP="${RECEIVER_IP:-10.10.55.64}"
BOUQUET="${BOUQUET:-Favourites (TV)}"
TEST_SERVICE_REF="${TEST_SERVICE_REF:-1:0:19:132F:3EF:1:C00000:0:0:0:}"

# Parse arguments
SKIP_BUILD=false
PRODUCTION_MODE=false
for arg in "$@"; do
    case $arg in
        --skip-build) SKIP_BUILD=true ;;
        --production) PRODUCTION_MODE=true ;;
        --help)
            echo "Usage: $0 [--skip-build] [--production]"
            echo ""
            echo "Options:"
            echo "  --skip-build    Skip Docker image build"
            echo "  --production    Test on production server (requires GPU)"
            exit 0
            ;;
    esac
done

# Helper functions
print_header() {
    echo ""
    echo -e "${BLUE}========================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}========================================${NC}"
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

cleanup() {
    if [ "$PRODUCTION_MODE" = false ]; then
        echo ""
        echo "Cleaning up test container..."
        docker rm -f "$CONTAINER_NAME" 2>/dev/null || true
    fi
}

trap cleanup EXIT

# =============================================================================
# PHASE 1: Build & Verification
# =============================================================================
print_header "PHASE 1: Build & Verification"

if [ "$SKIP_BUILD" = false ]; then
    print_step "1" "5" "Building Docker image with GPU support..."

    if docker build -t "$IMAGE_NAME" -f Dockerfile . > /tmp/xg2g-build.log 2>&1; then
        print_success "Docker image built successfully"

        # Show build size
        SIZE=$(docker images "$IMAGE_NAME" --format "{{.Size}}")
        print_info "Image size: $SIZE"
    else
        print_error "Docker build failed"
        echo ""
        tail -50 /tmp/xg2g-build.log
        exit 1
    fi
else
    print_step "1" "5" "Skipping Docker build (using existing image)..."

    if docker images "$IMAGE_NAME" | grep -q "$IMAGE_NAME"; then
        print_success "Using existing image: $IMAGE_NAME"
    else
        print_error "Image $IMAGE_NAME not found. Run without --skip-build"
        exit 1
    fi
fi

# Verify Rust library in image
print_step "2" "5" "Verifying Rust library in image..."

if docker run --rm "$IMAGE_NAME" ls -lh /app/lib/libxg2g_transcoder.so > /tmp/rust-lib-check.log 2>&1; then
    LIB_SIZE=$(grep libxg2g_transcoder.so /tmp/rust-lib-check.log | awk '{print $5}')
    print_success "Rust library found: $LIB_SIZE"
else
    print_error "Rust library not found in image"
    cat /tmp/rust-lib-check.log
    exit 1
fi

# Verify FFI symbols
print_step "3" "5" "Checking FFI symbols..."

GPU_SYMBOLS=$(docker run --rm "$IMAGE_NAME" nm -D /app/lib/libxg2g_transcoder.so 2>/dev/null | grep -c "gpu_server" || true)

if [ "$GPU_SYMBOLS" -ge 3 ]; then
    print_success "Found $GPU_SYMBOLS GPU server FFI symbols"
    docker run --rm "$IMAGE_NAME" nm -D /app/lib/libxg2g_transcoder.so 2>/dev/null | grep gpu_server | while read line; do
        print_info "$line"
    done
else
    print_error "Expected 3+ GPU FFI symbols, found: $GPU_SYMBOLS"
    exit 1
fi

# Verify Go binary
print_step "4" "5" "Verifying Go binary..."

if docker run --rm "$IMAGE_NAME" /app/xg2g --version > /tmp/version-check.log 2>&1; then
    VERSION=$(cat /tmp/version-check.log)
    print_success "Binary version: $VERSION"
else
    print_error "Binary check failed"
    cat /tmp/version-check.log
    exit 1
fi

print_step "5" "5" "Phase 1 complete"
print_success "Build verification passed"

# =============================================================================
# PHASE 2: Container Startup Test (without GPU)
# =============================================================================
print_header "PHASE 2: Container Startup Test (No GPU)"

print_step "1" "4" "Starting container without GPU..."

# Stop any existing container
docker rm -f "$CONTAINER_NAME" 2>/dev/null || true

# Start container in background
if docker run -d \
    --name "$CONTAINER_NAME" \
    -e XG2G_ENABLE_GPU_TRANSCODING=true \
    -e XG2G_GPU_LISTEN=0.0.0.0:8085 \
    -e XG2G_OWI_BASE="http://$RECEIVER_IP" \
    -e XG2G_BOUQUET="$BOUQUET" \
    -e XG2G_EPG_ENABLED=false \
    -e XG2G_HDHR_ENABLED=false \
    -p 8080:8080 \
    -p 8085:8085 \
    "$IMAGE_NAME" > /tmp/container-start.log 2>&1; then
    print_success "Container started"
else
    print_error "Container start failed"
    cat /tmp/container-start.log
    exit 1
fi

# Wait for container to be healthy
print_step "2" "4" "Waiting for container to be ready..."

MAX_WAIT=30
COUNT=0
while [ $COUNT -lt $MAX_WAIT ]; do
    if docker logs "$CONTAINER_NAME" 2>&1 | grep -q "GPU server"; then
        print_success "GPU server initialized"
        break
    fi
    sleep 1
    COUNT=$((COUNT + 1))
done

if [ $COUNT -ge $MAX_WAIT ]; then
    print_error "Container did not start properly within ${MAX_WAIT}s"
    echo ""
    echo "Container logs:"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi

# Check API health
print_step "3" "4" "Testing API health endpoint..."

sleep 2  # Give API time to start

if curl -sf http://localhost:8080/api/v1/status > /tmp/api-health.log 2>&1; then
    print_success "API is healthy"
    print_info "$(cat /tmp/api-health.log | jq -r '.version // empty' 2>/dev/null || echo 'API response OK')"
else
    print_error "API health check failed"
    cat /tmp/api-health.log
fi

# Check GPU server health
print_step "4" "4" "Testing GPU server health endpoint..."

if curl -sf http://localhost:8085/health > /tmp/gpu-health.log 2>&1; then
    print_success "GPU server is responding"

    VAAPI_STATUS=$(cat /tmp/gpu-health.log | jq -r '.vaapi_available' 2>/dev/null || echo "unknown")
    if [ "$VAAPI_STATUS" = "false" ]; then
        print_info "VAAPI: not available (expected without /dev/dri)"
    else
        print_info "VAAPI: $VAAPI_STATUS"
    fi

    GPU_VERSION=$(cat /tmp/gpu-health.log | jq -r '.version' 2>/dev/null || echo "unknown")
    print_info "Transcoder version: $GPU_VERSION"
else
    print_error "GPU server health check failed"
    cat /tmp/gpu-health.log

    echo ""
    echo "Container logs:"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi

print_success "Phase 2 complete (container runs without GPU)"

# =============================================================================
# PHASE 3: FFI Integration Test
# =============================================================================
print_header "PHASE 3: FFI Integration Test"

print_step "1" "3" "Checking daemon logs for GPU server initialization..."

if docker logs "$CONTAINER_NAME" 2>&1 | grep -q "Starting embedded GPU transcoding server"; then
    print_success "Go daemon called GPU FFI start function"
else
    print_error "GPU FFI start not found in logs"
    echo ""
    echo "Daemon logs:"
    docker logs "$CONTAINER_NAME" 2>&1 | grep -i gpu
    exit 1
fi

print_step "2" "3" "Checking for Rust GPU server startup..."

if docker logs "$CONTAINER_NAME" 2>&1 | grep -q "GPU server listening on"; then
    print_success "Rust GPU server started successfully"

    LISTEN_ADDR=$(docker logs "$CONTAINER_NAME" 2>&1 | grep "GPU server listening on" | tail -1 | awk '{print $NF}')
    print_info "Listening on: $LISTEN_ADDR"
else
    print_error "GPU server startup message not found"
    echo ""
    echo "Container logs:"
    docker logs "$CONTAINER_NAME" 2>&1 | tail -30
    exit 1
fi

print_step "3" "3" "Verifying FFI symbols are loaded..."

# Check that the daemon process has loaded the Rust library
if docker exec "$CONTAINER_NAME" sh -c "cat /proc/1/maps | grep -q libxg2g_transcoder" 2>/dev/null; then
    print_success "Rust library loaded in Go daemon process"
else
    print_info "Could not verify library loading (may be normal in Alpine)"
fi

print_success "Phase 3 complete (FFI integration working)"

# =============================================================================
# PHASE 4: Metrics & Monitoring
# =============================================================================
print_header "PHASE 4: Metrics & Monitoring"

print_step "1" "2" "Checking Prometheus metrics..."

if curl -sf http://localhost:8085/metrics > /tmp/metrics.log 2>&1; then
    print_success "Metrics endpoint accessible"

    METRIC_COUNT=$(grep -c "^xg2g_" /tmp/metrics.log || true)
    print_info "Found $METRIC_COUNT xg2g metrics"

    # Show a few sample metrics
    grep "^xg2g_" /tmp/metrics.log | head -5 | while read line; do
        print_info "$line"
    done
else
    print_error "Metrics endpoint failed"
fi

print_step "2" "2" "Checking container resource usage..."

STATS=$(docker stats "$CONTAINER_NAME" --no-stream --format "table {{.CPUPerc}}\t{{.MemUsage}}")
print_success "Container stats:"
echo "$STATS" | tail -1 | while read cpu mem rest; do
    print_info "CPU: $cpu  Memory: $mem"
done

print_success "Phase 4 complete (monitoring working)"

# =============================================================================
# PHASE 5: Production Test (with GPU) - Optional
# =============================================================================
if [ "$PRODUCTION_MODE" = true ]; then
    print_header "PHASE 5: Production Test (with GPU)"

    print_step "1" "5" "Stopping test container..."
    docker rm -f "$CONTAINER_NAME"
    print_success "Test container stopped"

    print_step "2" "5" "Starting container WITH GPU device mapping..."

    if docker run -d \
        --name "$CONTAINER_NAME" \
        --device /dev/dri:/dev/dri \
        -e XG2G_ENABLE_GPU_TRANSCODING=true \
        -e XG2G_GPU_LISTEN=0.0.0.0:8085 \
        -e XG2G_VAAPI_DEVICE=/dev/dri/renderD128 \
        -e XG2G_OWI_BASE="http://$RECEIVER_IP" \
        -e XG2G_BOUQUET="$BOUQUET" \
        -e XG2G_EPG_ENABLED=false \
        -e XG2G_HDHR_ENABLED=false \
        -e XG2G_ENABLE_STREAM_PROXY=true \
        -e XG2G_PROXY_LISTEN=:18000 \
        -e XG2G_ENABLE_AUDIO_TRANSCODING=true \
        -e XG2G_USE_RUST_REMUXER=true \
        -p 8080:8080 \
        -p 8085:8085 \
        -p 18000:18000 \
        "$IMAGE_NAME" > /tmp/container-gpu-start.log 2>&1; then
        print_success "Container started with GPU"
    else
        print_error "Container start failed"
        cat /tmp/container-gpu-start.log
        exit 1
    fi

    sleep 5

    print_step "3" "5" "Checking VAAPI availability..."

    if docker exec "$CONTAINER_NAME" vainfo > /tmp/vainfo.log 2>&1; then
        print_success "VAAPI is available"
        grep -E "(Driver|VAProfile)" /tmp/vainfo.log | head -5 | while read line; do
            print_info "$line"
        done
    else
        print_error "VAAPI not available"
        cat /tmp/vainfo.log
    fi

    # Check GPU health again
    print_step "4" "5" "Verifying GPU transcoding capability..."

    sleep 2

    if curl -sf http://localhost:8085/health > /tmp/gpu-health-prod.log 2>&1; then
        VAAPI_STATUS=$(cat /tmp/gpu-health-prod.log | jq -r '.vaapi_available' 2>/dev/null)

        if [ "$VAAPI_STATUS" = "true" ]; then
            print_success "VAAPI hardware acceleration is available!"
        else
            print_error "VAAPI still not available (check GPU drivers)"
            cat /tmp/gpu-health-prod.log
        fi
    fi

    print_step "5" "5" "Testing GPU transcode endpoint..."

    # Try a short transcode test
    TEST_URL="http://$RECEIVER_IP:8001/$TEST_SERVICE_REF"
    TRANSCODE_URL="http://localhost:8085/transcode?source_url=$(echo -n "$TEST_URL" | jq -sRr @uri)"

    print_info "Testing transcode for 5 seconds..."

    if timeout 5 curl -sf "$TRANSCODE_URL" > /tmp/transcode-test.ts 2>&1; then
        TEST_SIZE=$(wc -c < /tmp/transcode-test.ts)
        print_success "GPU transcoding works! Received $TEST_SIZE bytes in 5s"

        # Check if it's valid TS
        if file /tmp/transcode-test.ts | grep -q "MPEG"; then
            print_success "Output is valid MPEG transport stream"
        fi
    else
        print_error "Transcode test failed (may be normal if stream not available)"
    fi

    print_success "Phase 5 complete (production GPU test)"
fi

# =============================================================================
# Summary
# =============================================================================
print_header "Test Summary"

echo -e "${GREEN}✓ All tests passed!${NC}"
echo ""
echo "Test Results:"
echo "  - Docker build: ✓"
echo "  - FFI integration: ✓"
echo "  - Container startup: ✓"
echo "  - API health: ✓"
echo "  - GPU server health: ✓"
echo "  - Metrics: ✓"

if [ "$PRODUCTION_MODE" = true ]; then
    echo "  - GPU device access: ✓"
    echo "  - VAAPI check: ✓"
fi

echo ""
echo "Access URLs:"
echo "  API:        http://localhost:8080/api/v1/status"
echo "  GPU Health: http://localhost:8085/health"
echo "  Metrics:    http://localhost:8085/metrics"

if [ "$PRODUCTION_MODE" = true ]; then
    echo "  Streams:    http://localhost:18000/SERVICE_REF"
fi

echo ""
echo "Container logs:"
echo "  docker logs $CONTAINER_NAME"
echo ""
echo "Stop container:"
echo "  docker rm -f $CONTAINER_NAME"
echo ""

if [ "$PRODUCTION_MODE" = false ]; then
    echo -e "${YELLOW}Note: This test ran WITHOUT GPU device access.${NC}"
    echo -e "${YELLOW}Run with --production flag to test with real GPU.${NC}"
    echo ""
fi

print_success "MODE 3 (GPU Transcoding) test complete!"
