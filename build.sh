#!/bin/bash
# Build script for xg2g - ensures WebUI is properly built and copied before Docker build

set -e  # Exit on error

echo "🔨 Building xg2g..."

VERSION="$(git describe --tags --always --dirty 2>/dev/null || sed -n '1p' backend/VERSION 2>/dev/null || echo dev)"
COMMIT_HASH="$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
BUILD_DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# Step 1: Build WebUI
echo "📦 Building WebUI..."
cd frontend/webui
npm install && npm run build
cd ../..

# Step 2: Copy WebUI dist to embed location
echo "📋 Copying WebUI files to embed location..."
mkdir -p backend/internal/control/http/dist
rm -rf backend/internal/control/http/dist/*
cp -r frontend/webui/dist/* backend/internal/control/http/dist/

# Step 3: Build backend binary
echo "🐹 Building backend binary..."
cd backend
go build -buildvcs=false \
  -ldflags="-s -w -X github.com/ManuGH/xg2g/internal/version.Version=${VERSION} -X github.com/ManuGH/xg2g/internal/version.Commit=${COMMIT_HASH} -X github.com/ManuGH/xg2g/internal/version.Date=${BUILD_DATE}" \
  -o ../bin/xg2g ./cmd/daemon
cd ..

echo "✅ Build complete! Application running on http://localhost:8088"
