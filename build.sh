#!/bin/bash
# Build script for xg2g - ensures WebUI is properly built and copied before Docker build

set -e  # Exit on error

echo "🔨 Building xg2g..."

# Step 1: Build WebUI
echo "📦 Building WebUI..."
cd webui
npm run build
cd ..

# Step 2: Copy WebUI dist to embed location
echo "📋 Copying WebUI files to embed location..."
rm -rf internal/control/http/dist/*
cp -r webui/dist/* internal/control/http/dist/

# Step 3: Build Docker image
echo "🐳 Building Docker image..."
docker compose build xg2g

# Step 4: Start container
echo "🚀 Starting container..."
docker compose up -d

echo "✅ Build complete! Application running on http://localhost:8088"
