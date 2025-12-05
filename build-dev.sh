#!/bin/bash
# Development build script for xg2g on LXC

set -e

cd ~/xg2g
export PATH=$PATH:/usr/local/go/bin

echo "🔨 Building xg2g development version..."
echo ""

# Build WebUI
echo "📦 Building WebUI..."
cd webui
npm run build
cd ..

# Copy WebUI to internal/api/ui
echo "📋 Copying WebUI files..."
rm -rf internal/api/ui
mkdir -p internal/api/ui
cp -r webui/dist/* internal/api/ui/

# Build Go binary
echo "🔧 Building Go binary..."
/usr/local/go/bin/go build -o bin/daemon ./cmd/daemon

echo ""
echo "✅ Build complete!"
ls -lh bin/daemon
echo ""
echo "Run with: ./bin/daemon --help"
