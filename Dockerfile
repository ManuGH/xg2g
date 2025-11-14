# syntax=docker/dockerfile:1.7

# =============================================================================
# Auto-detect libc variant (Alpine/musl vs Debian/glibc)
# Usage: docker build --build-arg BASE_VARIANT=trixie   (default, glibc, FFmpeg 7.x)
#        docker build --build-arg BASE_VARIANT=bookworm (stable, FFmpeg 5.x)
#        docker build --build-arg BASE_VARIANT=alpine    (force Alpine, musl)
# =============================================================================
ARG BASE_VARIANT=trixie

# =============================================================================
# Stage 1: Build Rust Remuxer (ac-ffmpeg library for audio transcoding)
# =============================================================================
FROM rust:1.91-${BASE_VARIANT} AS rust-builder

WORKDIR /build

# Install FFmpeg development libraries and build tools
# Alpine: musl-dev, pkgconfig, ffmpeg-dev
# Debian: build-essential, pkg-config, libavcodec-dev, libavformat-dev, etc.
RUN if [ -f /etc/alpine-release ]; then \
        apk add --no-cache \
            musl-dev \
            pkgconfig \
            ffmpeg-dev \
            clang-dev \
            llvm-dev; \
    else \
        apt-get update && apt-get install -y \
            build-essential \
            pkg-config \
            libavcodec-dev \
            libavformat-dev \
            libavfilter-dev \
            libavdevice-dev \
            clang \
            && rm -rf /var/lib/apt/lists/*; \
    fi

# Set Cargo environment variables for caching
ENV CARGO_HOME=/usr/local/cargo \
    CARGO_TARGET_DIR=/build/target

# Copy Rust transcoder source
COPY transcoder/Cargo.toml ./
COPY transcoder/src ./src

# Build Rust remuxer library (cdylib for FFI) with BuildKit cache mounts
# This creates libxg2g_transcoder.so that Go can load via CGO
# Note: Cargo.lock is generated if missing (not committed to avoid library best practices)
# Note: On Alpine/musl, must disable crt-static to enable cdylib generation
# Note: Building without --lib to ensure Cargo.toml crate-type=[cdylib, rlib] is respected
# Note: BuildKit cache mounts dramatically speed up subsequent builds (40+ min → 5-10 min)
# Note: Three cache mounts: registry (crates), git (git deps), target (build artifacts)
# AMD64 CPU targets via feature flags (x86-64 microarchitecture levels)
# - v1 (baseline): SSE2 (no extra flags needed, default)
# - v2: +SSE3, SSSE3, SSE4.1, SSE4.2, POPCNT
# - v3: v2 + AVX, AVX2, BMI1, BMI2, FMA
# ARM64: Use generic target (no specific CPU level)
ARG RUST_TARGET_FEATURES=""
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target,id=rust-${BASE_VARIANT} \
    mkdir -p /output && \
    if [ -f /etc/alpine-release ]; then \
        RUSTFLAGS="-C target-cpu=generic ${RUST_TARGET_FEATURES} -C opt-level=3 -C target-feature=-crt-static" \
        cargo build --release; \
    else \
        RUSTFLAGS="-C target-cpu=generic ${RUST_TARGET_FEATURES} -C opt-level=3" \
        cargo build --release; \
    fi && \
    cp target/release/libxg2g_transcoder.so /output/ && \
    cp target/release/libxg2g_transcoder.rlib /output/
# Note: strip = true in Cargo.toml profile.release already strips the library
# Note: Files must be copied out of cache mount to be available in later stages

# =============================================================================
# Stage 2: Build Go Daemon with CGO (required for Rust FFI) + Run Tests
# =============================================================================
FROM golang:1.25-${BASE_VARIANT} AS go-builder

# Install build dependencies for CGO
# Note: Also installing runtime FFmpeg libraries to ensure matching versions for linking
# Bookworm: libavcodec59, libavformat59, libavfilter8, libavutil57, libswresample4
# Trixie:   libavcodec61, libavformat61, libavfilter10, libavutil59, libswresample5
RUN if [ -f /etc/alpine-release ]; then \
        apk add --no-cache \
            gcc \
            musl-dev \
            ffmpeg-dev; \
    else \
        apt-get update && apt-get install -y \
            gcc \
            libc6-dev \
            libavcodec-dev \
            libavformat-dev \
            libavfilter-dev \
            libavcodec61 \
            libavformat61 \
            libavfilter10 \
            libavutil59 \
            libswresample5 \
            && rm -rf /var/lib/apt/lists/*; \
    fi

# Build arguments for CPU optimization
# AMD64 levels: v1 (baseline 2003+), v2 (2009+, default), v3 (2015+, AVX2), v4 (2017+, AVX-512)
# ARM64: These arguments are ignored
ARG GO_AMD64_LEVEL=v2
ARG GO_GCFLAGS=""

WORKDIR /src

# Copy Rust library for CGO linking (from /output, not cache mount)
COPY --from=rust-builder /output/libxg2g_transcoder.so /usr/local/lib/
RUN if [ -f /etc/alpine-release ]; then \
        ldconfig /usr/local/lib 2>/dev/null || true; \
    else \
        ldconfig; \
    fi

# Copy Go source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build Go daemon WITH CGO enabled for Rust FFI bindings
# ALWAYS build with GPU support (-tags=gpu) - runtime decides what to use
ARG GIT_REF
ARG VERSION

# Single unified build: All features compiled in, runtime auto-detection
# - Rust remuxer: Always available (MODE 2)
# - GPU transcoding: Available if /dev/dri exists (MODE 3)
# - FFmpeg subprocess: Fallback for MODE 1
# Note: -extldflags='-Wl,-rpath,/app/lib' sets runtime library search path
# Note: CGO_LDFLAGS adds FFmpeg library path and explicit library linking
RUN set -eux; \
    BUILD_REF="${GIT_REF:-${VERSION:-dev}}"; \
    export CGO_ENABLED=1 GOOS=linux GOAMD64="${GO_AMD64_LEVEL}"; \
    export CGO_LDFLAGS="-L/usr/lib/x86_64-linux-gnu -lavcodec -lavformat -lavfilter -lavutil -lswresample"; \
    echo "🚀 Building unified binary with all features (Rust + GPU)"; \
    go build -tags=gpu -buildvcs=false -trimpath \
        -ldflags="-s -w -X 'main.Version=${BUILD_REF}' -extldflags='-Wl,-rpath,/app/lib'" \
        ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
        -o /out/xg2g ./cmd/daemon

# Verify build output
RUN ls -lh /out/xg2g && /out/xg2g --version || echo "Binary built successfully"

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
            ffmpeg-libs \
            libgcc && \
        addgroup -g 65532 -S xg2g && \
        adduser -u 65532 -S -G xg2g -h /app -s /bin/false xg2g; \
    else \
        apt-get update && apt-get install -y \
            ca-certificates \
            tzdata \
            wget \
            libavcodec61 \
            libavformat61 \
            libavfilter10 \
            libavutil59 \
            libswresample5 \
            && rm -rf /var/lib/apt/lists/* && \
        groupadd -g 65532 xg2g && \
        useradd -u 65532 -g xg2g -d /app -s /bin/false xg2g; \
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

RUN chmod +x /app/xg2g && \
    chown -R xg2g:xg2g /app

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

USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
