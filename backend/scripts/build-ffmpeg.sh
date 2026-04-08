#!/bin/bash
# FFmpeg Auto-Build Script for xg2g
# Builds pinned FFmpeg 8.1 with HLS/VAAPI/NVENC/x264/AAC support
set -euo pipefail

FFMPEG_VERSION="8.1"
FFMPEG_URL="https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"
NVCODEC_HEADERS_VERSION="n13.0.19.0"
NVCODEC_HEADERS_REPO="https://git.videolan.org/git/ffmpeg/nv-codec-headers.git"
TARGET_DIR="${TARGET_DIR:-/opt/ffmpeg}"
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
EXPECTED_SHA256="b072aed6871998cce9b36e7774033105ca29e33632be5b6347f3206898e0756a"
VERIFY_LINE="${EXPECTED_SHA256}  ffmpeg-${FFMPEG_VERSION}.tar.xz"
verify_checksum() {
    if command -v sha256sum >/dev/null 2>&1; then
        echo "${VERIFY_LINE}" | sha256sum -c -
        return $?
    fi
    if command -v shasum >/dev/null 2>&1; then
        echo "${VERIFY_LINE}" | shasum -a 256 -c -
        return $?
    fi
    echo "ERROR: sha256sum or shasum is required to verify the FFmpeg source checksum." >&2
    return 2
}

if ! verify_checksum; then
    if [ "${ALLOW_CHECKSUM_MISMATCH:-}" = "1" ]; then
        echo "WARNING: Checksum verification failed, but ALLOW_CHECKSUM_MISMATCH=1 is set. Proceeding anyway." >&2
    else
        echo "ERROR: Checksum verification failed. Refusing to build. (Update EXPECTED_SHA256 if FFmpeg source changed.)" >&2
        exit 1
    fi
fi

# Extract
echo "Extracting..."
tar xf "ffmpeg-${FFMPEG_VERSION}.tar.xz"

# Install pinned NVENC headers required by FFmpeg's ffnvcodec detection.
rm -rf nv-codec-headers
echo "Cloning nv-codec-headers ${NVCODEC_HEADERS_VERSION}..."
git clone --branch "${NVCODEC_HEADERS_VERSION}" --depth 1 "${NVCODEC_HEADERS_REPO}" nv-codec-headers
echo "Installing nv-codec-headers ${NVCODEC_HEADERS_VERSION}..."
make -C nv-codec-headers PREFIX=/usr/local install

cd "ffmpeg-${FFMPEG_VERSION}"

# Configure
echo "Configuring FFmpeg..."
./configure \
  --prefix="${TARGET_DIR}" \
  --enable-gpl \
  --enable-libx264 \
  --enable-libx265 \
  --enable-vaapi \
  --enable-nvenc \
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
make -j"$(nproc)"

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
echo "Or set:"
echo "  export XG2G_FFMPEG_BIN=${TARGET_DIR}/bin/ffmpeg"
echo "  export XG2G_FFPROBE_BIN=${TARGET_DIR}/bin/ffprobe"
