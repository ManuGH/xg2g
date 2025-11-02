# syntax=docker/dockerfile:1.7

# =============================================================================
# Stage 1: Build Rust Remuxer (ac-ffmpeg library for audio transcoding)
# =============================================================================
FROM rust:1.84-alpine AS rust-builder

WORKDIR /build

# Install FFmpeg development libraries and build tools for Alpine
RUN apk add --no-cache \
    musl-dev \
    pkgconfig \
    ffmpeg-dev \
    clang-dev \
    llvm-dev

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
# AMD64 CPU targets: x86-64 (v1), x86-64-v2, x86-64-v3, x86-64-v4
# ARM64: Use generic target (no specific CPU level)
ARG RUST_TARGET_CPU=x86-64-v2
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target \
    mkdir -p /output && \
    RUSTFLAGS="-C target-cpu=${RUST_TARGET_CPU} -C opt-level=3 -C target-feature=-crt-static" \
    cargo build --release && \
    cp target/release/libxg2g_transcoder.so /output/ && \
    cp target/release/libxg2g_transcoder.rlib /output/
# Note: strip = true in Cargo.toml profile.release already strips the library
# Note: Files must be copied out of cache mount to be available in later stages

# =============================================================================
# Stage 2: Build Go Daemon with CGO (required for Rust FFI) + Run Tests
# =============================================================================
FROM golang:1.25-alpine AS go-builder

# Install build dependencies for CGO
RUN apk add --no-cache \
    gcc \
    musl-dev \
    ffmpeg-dev

# Build arguments for CPU optimization
# AMD64 levels: v1 (baseline 2003+), v2 (2009+, default), v3 (2015+, AVX2), v4 (2017+, AVX-512)
# ARM64: These arguments are ignored
ARG GO_AMD64_LEVEL=v2
ARG GO_GCFLAGS=""

WORKDIR /src

# Copy Rust library for CGO linking (from /output, not cache mount)
COPY --from=rust-builder /output/libxg2g_transcoder.so /usr/local/lib/
RUN ldconfig /usr/local/lib 2>/dev/null || true

# Copy Go source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build Go daemon WITH CGO enabled for Rust FFI bindings
ARG GIT_REF
ARG VERSION
ARG ENABLE_GPU=false

# Conditional build: GPU tag only when ENABLE_GPU=true
# Standard mode (MODE 1+2): Audio transcoding via Rust, no GPU
# GPU mode (MODE 3): Audio + video transcoding via VAAPI
# Note: -extldflags='-Wl,-rpath,/app/lib' sets runtime library search path
RUN set -eux; \
    BUILD_REF="${GIT_REF:-${VERSION:-dev}}"; \
    export CGO_ENABLED=1 GOOS=linux GOAMD64="${GO_AMD64_LEVEL}"; \
    if [ "$ENABLE_GPU" = "true" ]; then \
        echo "🎮 Building with GPU transcoding support (MODE 3)"; \
        go build -tags=gpu -buildvcs=false -trimpath \
            -ldflags="-s -w -X 'main.Version=${BUILD_REF}' -extldflags='-Wl,-rpath,/app/lib'" \
            ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
            -o /out/xg2g ./cmd/daemon; \
    else \
        echo "📺 Building without GPU transcoding (MODE 1+2 only)"; \
        go build -buildvcs=false -trimpath \
            -ldflags="-s -w -X 'main.Version=${BUILD_REF}' -extldflags='-Wl,-rpath,/app/lib'" \
            ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
            -o /out/xg2g ./cmd/daemon; \
    fi

# Verify build output
RUN ls -lh /out/xg2g && /out/xg2g --version || echo "Binary built successfully"

# =============================================================================
# Stage 3: Runtime Image with Audio Transcoding
# =============================================================================
FROM alpine:3.22.2

# Install runtime dependencies
# - ffmpeg-libs: Required by Rust remuxer (libavcodec, libavformat, etc.)
# - libgcc: Required by Rust binary
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg-libs \
    libgcc && \
  addgroup -g 65532 -S xg2g && \
  adduser -u 65532 -S -G xg2g -h /app -s /bin/false xg2g && \
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

# Default configuration (minimal - standard mode)
# Audio transcoding and stream proxy are DISABLED by default
# Enable via environment variables for iPhone/iPad mode
ENV XG2G_DATA=/data \
  XG2G_LISTEN=:8080 \
  XG2G_OWI_BASE=http://192.168.1.100 \
  XG2G_BOUQUET=Favourites \
  XG2G_FUZZY_MAX=2

USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
