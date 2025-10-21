#!/bin/bash
# Build script with CPU-specific optimizations for AMD Ryzen 7 8745HS (Zen 4)
# This script builds optimized images for your specific system

set -e

echo "🚀 Building xg2g with AMD Zen 4 optimizations..."
echo ""

# Detect CPU
CPU_MODEL=$(lscpu | grep "Model name" | cut -d: -f2 | xargs)
echo "Detected CPU: $CPU_MODEL"
echo ""

# Build Rust transcoder with Zen 4 optimizations
echo "📦 Building Rust GPU Transcoder (target-cpu=znver4)..."
docker build \
  --build-arg RUST_TARGET_CPU=znver4 \
  --build-arg RUST_OPT_LEVEL=3 \
  -t xg2g-gpu-transcoder:production \
  ./transcoder

echo "✅ Transcoder built successfully"
echo ""

# Build Go service with GOAMD64=v3 optimizations
echo "📦 Building Go service (GOAMD64=v3)..."
docker build \
  --build-arg GO_AMD64_LEVEL=v3 \
  --build-arg GO_GCFLAGS="all=-spectre=ret" \
  -t xg2g:latest \
  .

echo "✅ Go service built successfully"
echo ""

echo "🎉 All images built with AMD Zen 4 optimizations!"
echo ""
echo "Optimizations applied:"
echo "  - Rust: target-cpu=znver4 (AVX512, VAES, VPCLMULQDQ)"
echo "  - Go:   GOAMD64=v3 (AVX2, BMI2, FMA)"
echo ""
echo "To start services:"
echo "  cd /opt/stacks/xg2g-gpu"
echo "  docker compose -f docker-compose.minimal.yml up -d"
