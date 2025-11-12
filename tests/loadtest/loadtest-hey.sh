#!/bin/bash
# xg2g Load Test using hey
# Simple HTTP load generator for quick performance checks

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
BASE_URL="${XG2G_URL:-http://localhost:8080}"
REQUESTS=10000
CONCURRENCY=100
TIMEOUT=30

echo "========================================"
echo "xg2g Load Test (hey)"
echo "========================================"
echo "Base URL: $BASE_URL"
echo "Requests: $REQUESTS"
echo "Concurrency: $CONCURRENCY"
echo "========================================"
echo ""

# Check if hey is installed
if ! command -v hey &> /dev/null; then
    echo -e "${RED}Error: 'hey' is not installed${NC}"
    echo "Install with: brew install hey"
    exit 1
fi

# Check if service is up
echo -e "${YELLOW}Checking if service is up...${NC}"
if ! curl -sf "${BASE_URL}/healthz" > /dev/null; then
    echo -e "${RED}Error: Service not reachable at ${BASE_URL}${NC}"
    exit 1
fi
echo -e "${GREEN}✓ Service is up${NC}"
echo ""

# Test 1: Health Check
echo "========================================"
echo "Test 1: Health Check (/healthz)"
echo "========================================"
hey -n $REQUESTS -c $CONCURRENCY -t $TIMEOUT "${BASE_URL}/healthz"
echo ""

# Test 2: Metrics Endpoint
echo "========================================"
echo "Test 2: Metrics Endpoint (/metrics)"
echo "========================================"
hey -n 1000 -c 20 -t $TIMEOUT "${BASE_URL}/metrics"
echo ""

# Test 3: API Status
echo "========================================"
echo "Test 3: API Status (/api/status)"
echo "========================================"
hey -n 5000 -c 50 -t $TIMEOUT "${BASE_URL}/api/status"
echo ""

# Test 4: M3U Playlist (if available)
if curl -sf "${BASE_URL}/playlist.m3u" > /dev/null 2>&1; then
    echo "========================================"
    echo "Test 4: M3U Playlist (/playlist.m3u)"
    echo "========================================"
    hey -n 1000 -c 20 -t $TIMEOUT "${BASE_URL}/playlist.m3u"
    echo ""
else
    echo -e "${YELLOW}Skipping M3U test (not configured)${NC}"
    echo ""
fi

# Test 5: Sustained Load
echo "========================================"
echo "Test 5: Sustained Load (30s duration)"
echo "========================================"
hey -z 30s -c 50 -t $TIMEOUT "${BASE_URL}/healthz"
echo ""

echo "========================================"
echo -e "${GREEN}Load test completed!${NC}"
echo "========================================"
