#!/bin/bash
# FFmpeg Auto-Build Script for xg2g
# Builds pinned FFmpeg 8.1.2 with HLS/VAAPI/NVENC/x264/AAC support
set -euo pipefail

FFMPEG_VERSION="8.1.2"
FFMPEG_URL="https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz"
NVCODEC_HEADERS_VERSION="n13.0.19.0"
NVCODEC_HEADERS_REPO="https://github.com/FFmpeg/nv-codec-headers.git"
TARGET_DIR="${TARGET_DIR:-/opt/ffmpeg}"
BUILD_DIR="${BUILD_DIR:-/tmp/ffmpeg-build}"

echo "=== Building FFmpeg ${FFMPEG_VERSION} ==="

# Create build directory
mkdir -p "${BUILD_DIR}"
cd "${BUILD_DIR}"

# Download FFmpeg source
if [ ! -f "ffmpeg-${FFMPEG_VERSION}.tar.xz" ]; then
    echo "Downloading FFmpeg ${FFMPEG_VERSION}..."
    curl -fsSL --retry 5 --retry-delay 2 --retry-connrefused --connect-timeout 20 "${FFMPEG_URL}" -o "ffmpeg-${FFMPEG_VERSION}.tar.xz"
fi

# Verify checksum (sha256)
echo "Verifying checksum..."
EXPECTED_SHA256="464beb5e7bf0c311e68b45ae2f04e9cc2af88851abb4082231742a74d97b524c"
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
if ! git clone --branch "${NVCODEC_HEADERS_VERSION}" --depth 1 "${NVCODEC_HEADERS_REPO}" nv-codec-headers; then
    echo "Fallback: cloning nv-codec-headers from videolan mirror..."
    git clone --branch "${NVCODEC_HEADERS_VERSION}" --depth 1 "https://git.videolan.org/git/ffmpeg/nv-codec-headers.git" nv-codec-headers
fi
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
  --enable-libdav1d \
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

# --- Split debug info -------------------------------------------------------
#
# This build used to pass --disable-debug, which drops -g. The cost of that only
# becomes visible when something goes wrong: ffmpeg faulted six times on the
# staging deployment over four months and left 716MB of core files, and not one
# could be attributed to a function because the binaries carry no symbols. Four
# of those cores are the same null dereference at the same address — very likely
# a single fixable bug that has simply never been readable.
#
# What ships is deliberately unchanged. The runtime binaries are stripped of
# DWARF only (--strip-debug), so their dynamic symbol table, size and load
# behaviour stay exactly as before; the debug info moves to a separate tree that
# the runtime image never copies. The tree is laid out the way debuggers look
# symbols up — by build ID — so a core from a given image resolves against the
# symbols from that same build with no path juggling and no chance of pairing a
# core with the wrong build.
#
# Cost: the compile carries -g, so the builder stage takes longer and needs more
# scratch space. The base image is built rarely; a crash that cannot be read
# costs more.
DEBUG_DIR="${DEBUG_DIR:-/opt/ffmpeg-debug}"
DEBUG_ROOT="${DEBUG_DIR}/.build-id"
mkdir -p "${DEBUG_ROOT}"

build_id_of() {
  readelf -n "$1" 2>/dev/null | sed -n 's/.*Build ID: \([0-9a-f]\{4,\}\).*/\1/p' | head -1
}

split_debug() {
  local f="$1" id short rest
  id="$(build_id_of "$f")"
  if [ -z "$id" ]; then
    echo "  warn: no build ID, shipping without separable symbols: $f" >&2
    return 0
  fi
  short="${id:0:2}"
  rest="${id:2}"
  mkdir -p "${DEBUG_ROOT}/${short}"
  objcopy --only-keep-debug "$f" "${DEBUG_ROOT}/${short}/${rest}.debug"
  # DWARF only: keep .dynsym so the shipped artifact matches the previous build.
  strip --strip-debug "$f"
}

echo "Splitting debug symbols by build ID into ${DEBUG_DIR}..."
for f in "${TARGET_DIR}"/bin/* "${TARGET_DIR}"/lib/*.so.*; do
  [ -f "$f" ] || continue
  case "$f" in *.debug) continue ;; esac
  split_debug "$f"
done
echo "Debug symbols: $(find "${DEBUG_ROOT}" -name '*.debug' | wc -l) files, $(du -sh "${DEBUG_DIR}" | cut -f1)"
cat > "${DEBUG_DIR}/README" <<EOF
FFmpeg ${FFMPEG_VERSION} debug symbols, keyed by GNU build ID.

These pair with the stripped binaries in ${TARGET_DIR} of the SAME image build.
To read a core:

  gdb -ex 'set debug-file-directory ${DEBUG_DIR}' \\
      -ex 'bt full' --batch /opt/ffmpeg/bin/ffmpeg <core-file>

Confirm the pairing first — the build ID in the core's NT_FILE mapping must have
a matching .build-id/xx/rest.debug entry here. A core from a different image
build will not resolve, and gdb will say so rather than lie.
EOF

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
