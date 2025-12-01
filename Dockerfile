# syntax=docker/dockerfile:1.7

# =============================================================================
# Auto-detect libc variant (Alpine/musl vs Debian/glibc)
# Usage: docker build --build-arg BASE_VARIANT=trixie   (default, glibc, FFmpeg 7.x)
#        docker build --build-arg BASE_VARIANT=bookworm (stable, FFmpeg 5.x)
#        docker build --build-arg BASE_VARIANT=alpine    (force Alpine, musl)
# =============================================================================
ARG BASE_VARIANT=bookworm

# =============================================================================
# Stage 0: Cross-compilation helpers
# =============================================================================
FROM --platform=$BUILDPLATFORM tonistiigi/xx:1.8.0 AS xx

# =============================================================================
# Stage 1: Build Rust Remuxer (ac-ffmpeg library for audio transcoding)
# =============================================================================
FROM --platform=$BUILDPLATFORM rust:1.91-${BASE_VARIANT} AS rust-builder

# Copy xx helpers
COPY --from=xx / /

# Target platform arguments provided by Docker Buildx
ARG TARGETPLATFORM
ARG TARGETARCH

WORKDIR /build

# Install FFmpeg development libraries and build tools (cross-compiled)
# Alpine: musl-dev, pkgconfig, ffmpeg-dev
# Debian: build-essential, pkg-config, libavcodec-dev, libavformat-dev, etc.
RUN if [ -f /etc/alpine-release ]; then \
    # Alpine cross-compilation setup
    xx-apk add --no-cache \
    musl-dev \
    pkgconfig \
    ffmpeg-dev \
    clang-dev \
    llvm-dev \
    gcc; \
    else \
    # Debian cross-compilation setup
    # 1. Install native build tools and FFmpeg dev headers (for build scripts that need headers)
    apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    clang \
    libavcodec-dev \
    libavformat-dev \
    libavfilter-dev \
    libavutil-dev \
    libswresample-dev \
    && \
    # 2. Install target libraries and cross-compiler helpers via xx
    xx-apt-get update && xx-apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    libavcodec-dev \
    libavformat-dev \
    libavfilter-dev \
    libavutil-dev \
    libswresample-dev \
    && rm -rf /var/lib/apt/lists/*; \
    fi

# Set Cargo environment variables for caching
ENV CARGO_HOME=/usr/local/cargo \
    CARGO_TARGET_DIR=/build/target

# Set FFMPEG_INCLUDE_DIR and FFMPEG_LIB_DIR for ac-ffmpeg crate
# For both Alpine and Debian, FFmpeg headers/libs are in standard paths
# During cross-compilation, xx-cargo will handle the sysroot paths automatically
ENV FFMPEG_INCLUDE_DIR=/usr/include \
    FFMPEG_LIB_DIR=/usr/lib

# Copy Rust transcoder source
COPY transcoder/Cargo.toml ./
COPY transcoder/src ./src

# Build Rust remuxer library (cdylib for FFI) with BuildKit cache mounts
# This creates libxg2g_transcoder.so that Go can load via CGO
# Note: xx-cargo automatically handles target architecture configuration
ARG RUST_TARGET_FEATURES=""
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target,id=rust-${BASE_VARIANT}-${TARGETARCH} \
    mkdir -p /output && \
    if [ -f /etc/alpine-release ]; then \
    RUSTFLAGS="-C target-cpu=generic ${RUST_TARGET_FEATURES} -C opt-level=3 -C target-feature=-crt-static" \
    xx-cargo build --release --lib --target-dir target; \
    else \
    # For Debian: FFmpeg headers are installed natively, FFMPEG_INCLUDE_DIR points to them
    # xx-cargo handles cross-compilation, linking uses target sysroot libraries
    # Build only the library (--lib), not the binary which needs additional deps
    RUSTFLAGS="-C target-cpu=generic ${RUST_TARGET_FEATURES} -C opt-level=3" \
    xx-cargo build --release --lib --target-dir target; \
    fi && \
    # xx-cargo puts artifacts in target/<triple>/release/ or target/release/ depending on cross setup
    # Debug: find where the artifacts actually are
    echo "Searching for artifacts..." && \
    find target -name "libxg2g_transcoder.so" -o -name "libxg2g_transcoder.rlib" && \
    # Copy artifacts using find to locate them
    SO_FILE=$(find target -name "libxg2g_transcoder.so" -type f | head -n1) && \
    RLIB_FILE=$(find target -name "libxg2g_transcoder.rlib" -type f | head -n1) && \
    if [ -z "$SO_FILE" ]; then echo "Error: .so file not found"; exit 1; fi && \
    if [ -z "$RLIB_FILE" ]; then echo "Error: .rlib file not found"; exit 1; fi && \
    echo "Copying $SO_FILE and $RLIB_FILE to /output/" && \
    cp "$SO_FILE" /output/ && \
    cp "$RLIB_FILE" /output/

# =============================================================================
# Stage 1.5: Build WebUI (React + Vite)
# =============================================================================
FROM --platform=$BUILDPLATFORM node:20-alpine AS node-builder
WORKDIR /webui
COPY webui/package*.json ./
RUN npm ci
COPY webui/ .
RUN npm run build

# =============================================================================
# Stage 2: Build Go Daemon with CGO (required for Rust FFI) + Run Tests
# =============================================================================
FROM --platform=$BUILDPLATFORM golang:1.25-${BASE_VARIANT} AS go-builder

# Copy xx helpers
COPY --from=xx / /

# Target platform arguments
ARG TARGETPLATFORM
ARG TARGETARCH

# Install build dependencies for CGO (cross-compiled)
# Note: Also installing runtime FFmpeg libraries to ensure matching versions for linking
RUN if [ -f /etc/alpine-release ]; then \
    xx-apk add --no-cache \
    gcc \
    musl-dev \
    ffmpeg-dev; \
    else \
    # 1. Install native build tools
    apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    && \
    # 2. Install target libraries and cross-compiler helpers
    xx-apt-get update && xx-apt-get install -y --no-install-recommends \
    gcc \
    libc6-dev \
    libavcodec-dev \
    libavformat-dev \
    libavfilter-dev \
    libavutil-dev \
    libswresample-dev \
    && rm -rf /var/lib/apt/lists/*; \
    fi

# Build arguments for CPU optimization
ARG GO_AMD64_LEVEL=v2
ARG GO_GCFLAGS=""

WORKDIR /src

# Copy Rust library for CGO linking (from /output, not cache mount)
COPY --from=rust-builder /output/libxg2g_transcoder.so /usr/local/lib/

# Link library for cross-compilation context
# We need to ensure the linker finds the library for the TARGET architecture
RUN if [ -f /etc/alpine-release ]; then \
    # Alpine
    mkdir -p /usr/$(xx-info triple)/lib && \
    cp /usr/local/lib/libxg2g_transcoder.so /usr/$(xx-info triple)/lib/ && \
    ldconfig /usr/$(xx-info triple)/lib 2>/dev/null || true; \
    else \
    # Debian
    mkdir -p /usr/lib/$(xx-info triple) && \
    cp /usr/local/lib/libxg2g_transcoder.so /usr/lib/$(xx-info triple)/ && \
    ldconfig; \
    fi

# Copy Go source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Copy WebUI artifacts from node-builder
COPY --from=node-builder /webui/dist ./internal/api/ui

# Build Go daemon WITH CGO enabled for Rust FFI bindings
ARG GIT_REF
ARG VERSION
ARG BUILD_REVISION=unknown

# Build with Rust remuxer (MODE 2)
# xx-go handles GOOS, GOARCH, CC, CXX, and CGO_ENABLED automatically
RUN set -eux; \
    BUILD_REF="${GIT_REF:-${VERSION:-dev}}"; \
    export GOAMD64="${GO_AMD64_LEVEL}"; \
    # Explicitly set CGO_LDFLAGS to help cross-compiler find libraries
    export CGO_LDFLAGS="-L/usr/lib/$(xx-info triple) -L/usr/$(xx-info triple)/lib -lxg2g_transcoder -lavcodec -lavformat -lavfilter -lavutil -lswresample"; \
    echo "🚀 Building binary with Rust remuxer (MODE 2) for $TARGETPLATFORM"; \
    xx-go build -buildvcs=false -trimpath -tags=gpu \
    -ldflags="-s -w -X 'main.Version=${BUILD_REF}' -extldflags='-Wl,-rpath,/app/lib'" \
    ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
    -o /out/xg2g ./cmd/daemon

# Verify build output (check architecture)
RUN xx-verify /out/xg2g

# =============================================================================
# Stage 3: Runtime Image with Audio Transcoding
# =============================================================================
FROM alpine:3.22.2 AS runtime-alpine
FROM debian:bookworm-slim AS runtime-bookworm
FROM debian:trixie-slim AS runtime-trixie

# Select runtime based on variant
FROM runtime-${BASE_VARIANT} AS runtime

# Install runtime dependencies
# Alpine: ffmpeg-libs, libgcc
# Debian Bookworm: libavcodec59, libavformat59, libavfilter8, libavutil57, libswresample4
# Debian Trixie:   libavcodec61, libavformat61, libavfilter10, libavutil59, libswresample5
RUN if [ -f /etc/alpine-release ]; then \
    apk add --no-cache \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg \
    ffmpeg-libs \
    libgcc && \
    addgroup -g 65532 -S xg2g && \
    adduser -u 65532 -S -G xg2g -h /app -s /bin/false xg2g && \
    addgroup xg2g video && \
    (getent group render || addgroup -S render) && \
    addgroup xg2g render; \
    else \
    apt-get update && apt-get install -y \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg \
    libavcodec59 \
    libavformat59 \
    libavfilter8 \
    libavutil57 \
    libswresample4 \
    && rm -rf /var/lib/apt/lists/* && \
    groupadd -g 65532 xg2g && \
    useradd -u 65532 -g xg2g -d /app -s /bin/false xg2g && \
    usermod -aG video xg2g && \
    (getent group render || groupadd -r render) && \
    usermod -aG render xg2g; \
    fi && \
    mkdir -p /data /app/lib && \
    chown -R xg2g:xg2g /data /app

WORKDIR /app

# Copy Go daemon (dynamically linked with Rust library)
COPY --from=go-builder /out/xg2g .

# Copy Rust remuxer library (from /output, not cache mount)
COPY --from=rust-builder /output/libxg2g_transcoder.so ./lib/

# Set library path for Rust remuxer
ENV LD_LIBRARY_PATH=/app/lib

# Cache busting: BUILD_REVISION changes on every commit, forcing rebuild of final layers
ARG BUILD_REVISION=unknown
RUN chmod +x /app/xg2g && \
    chown -R xg2g:xg2g /app && \
    echo "Build: ${BUILD_REVISION}" > /app/.build-info

VOLUME ["/data"]

# Expose API port (8080) and Stream Proxy port (18000)
EXPOSE 8080 18000

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/status || exit 1

# Default configuration - Standard mode (MODE 1)
# Audio transcoding and stream proxy DISABLED by default
# See docker-compose.yml for MODE 2 (Audio) and MODE 3 (GPU)
ENV XG2G_DATA=/data \
    XG2G_LISTEN=:8080 \
    XG2G_OWI_BASE=http://192.168.1.100 \
    XG2G_BOUQUET=Favourites \
    XG2G_FUZZY_MAX=2

# Image metadata
LABEL org.opencontainers.image.revision="${BUILD_REVISION}" \
    org.opencontainers.image.source="https://github.com/ManuGH/xg2g" \
    org.opencontainers.image.description="Enigma2 to IPTV Gateway with Rust-powered audio transcoding"

# NOTE: Run as root for LXC compatibility (Proxmox, etc.)
# Docker+LXC+non-root user triggers sysctl errors: "open sysctl net.ipv4.ip_unprivileged_port_start: permission denied"
# Running as root in an LXC container is safe (container itself provides isolation)
# To enable root for LXC: Set 'user: root' in docker-compose.yml
USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
