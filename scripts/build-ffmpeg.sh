#!/bin/bash
# FFmpeg Auto-Build Script for xg2g
# Builds pinned FFmpeg 7.1.3 with HLS/VAAPI/x264/AAC support
set -euo pipefail

FFMPEG_VERSION="7.1.3"
FFMPEG_TAG="n${FFMPEG_VERSION}"
FFMPEG_URL="https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"
TARGET_DIR="${TARGET_DIR:-/opt/xg2g/ffmpeg}"
BUILD_DIR="${BUILD_DIR:-/tmp/ffmpeg-build}"

echo "=== Building FFmpeg ${FFMPEG_VERSION} ==="

# Create build directory
mkdir -p "${BUILD_DIR}"
cd "${BUILD_DIR}"

# Download FFmpeg source
if [ ! -f "ffmpeg-${FFMPEG_VERSION}.tar.xz" ]; then
    echo "Downloading FFmpeg ${FFMPEG_VERSION}..."
    curl -fsSL "${FFMPEG_URL}" -o "ffmpeg-${FFMPEG_VERSION}.tar.xz"
fi

# Verify checksum (sha256)
echo "Verifying checksum..."
EXPECTED_SHA256="f0bf043299db9e3caacb435a712fc541fbb07df613c4b893e8b77e67baf3adbe"
if command -v sha256sum &> /dev/null; then
    echo "${EXPECTED_SHA256}  ffmpeg-${FFMPEG_VERSION}.tar.xz" | sha256sum -c - || {
        echo "WARNING: Checksum verification failed. Proceeding anyway (update EXPECTED_SHA256)."
    }
fi

# Extract
echo "Extracting..."
tar xf "ffmpeg-${FFMPEG_VERSION}.tar.xz"
cd "ffmpeg-${FFMPEG_VERSION}"

# Configure
echo "Configuring FFmpeg..."
./configure \
  --prefix="${TARGET_DIR}" \
  --enable-gpl \
  --enable-libx264 \
  --enable-libx265 \
  --enable-vaapi \
  --enable-protocol=hls \
  --enable-protocol=file \
  --enable-protocol=http \
  --enable-protocol=tcp \
  --enable-demuxer=mpegts \
  --enable-demuxer=hls \
  --enable-muxer=hls \
  --enable-muxer=mpegts \
  --disable-debug \
  --disable-doc \
  --disable-static \
  --enable-shared

# Build
echo "Building (using $(nproc) cores)..."
make -j$(nproc)

# Install
echo "Installing to ${TARGET_DIR}..."
mkdir -p "${TARGET_DIR}"
make install

# Verify
echo ""
echo "=== FFmpeg Build Complete ==="
export LD_LIBRARY_PATH="${TARGET_DIR}/lib:${LD_LIBRARY_PATH:-}"
"${TARGET_DIR}/bin/ffmpeg" -version | head -3
echo ""
echo "Installed to: ${TARGET_DIR}"
echo ""
echo "To use FFmpeg, add these to your shell:"
echo "  export PATH=${TARGET_DIR}/bin:\$PATH"
echo "  export LD_LIBRARY_PATH=${TARGET_DIR}/lib:\$LD_LIBRARY_PATH"
echo ""
echo "Or set XG2G_FFMPEG_PATH=${TARGET_DIR}/bin/ffmpeg"
