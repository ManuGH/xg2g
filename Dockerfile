# syntax=docker/dockerfile:1.7

# =============================================================================
# xg2g - Enigma2 to IPTV Gateway with Rust Audio Transcoding
# Target: Linux amd64 only, Debian Trixie (FFmpeg 7.x)
# =============================================================================

# =============================================================================
# Stage 1: Build Rust Remuxer (ac-ffmpeg library for audio transcoding)
# =============================================================================
FROM rust:1.91-trixie AS rust-builder

WORKDIR /build

# Install FFmpeg 7.x development libraries
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    pkg-config \
    clang \
    libavcodec-dev \
    libavformat-dev \
    libavfilter-dev \
    libavutil-dev \
    libswresample-dev \
    && rm -rf /var/lib/apt/lists/*

# Set Cargo environment variables for caching
ENV CARGO_HOME=/usr/local/cargo \
    CARGO_TARGET_DIR=/build/target

# Set FFmpeg paths for ac-ffmpeg crate
ENV FFMPEG_INCLUDE_DIR=/usr/include \
    FFMPEG_LIB_DIR=/usr/lib

# Copy Rust transcoder source
COPY transcoder/Cargo.toml ./
COPY transcoder/src ./src

# Build Rust remuxer library (cdylib for FFI) with BuildKit cache mounts
# This creates libxg2g_transcoder.so that Go can load via CGO
ARG RUST_TARGET_FEATURES=""
RUN --mount=type=cache,target=/usr/local/cargo/registry \
    --mount=type=cache,target=/usr/local/cargo/git \
    --mount=type=cache,target=/build/target,id=rust-trixie-amd64 \
    RUSTFLAGS="-C target-cpu=x86-64-v2 ${RUST_TARGET_FEATURES} -C opt-level=3" \
    cargo build --release --lib && \
    mkdir -p /output && \
    cp target/release/libxg2g_transcoder.so /output/ && \
    cp target/release/libxg2g_transcoder.rlib /output/

# =============================================================================
# Stage 2: Build WebUI (React + Vite)
# =============================================================================
FROM node:25-alpine AS node-builder
WORKDIR /webui
COPY webui/package*.json ./
RUN npm ci
COPY webui/ .
RUN npm run build

# =============================================================================
# Stage 3: Build Go Daemon with CGO (required for Rust FFI)
# =============================================================================
FROM golang:1.25-trixie AS go-builder

# Install build dependencies for CGO and FFmpeg 7.x libs
RUN apt-get update && apt-get install -y --no-install-recommends \
    pkg-config \
    gcc \
    libc6-dev \
    libavcodec-dev \
    libavformat-dev \
    libavfilter-dev \
    libavutil-dev \
    libswresample-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Copy Rust library for CGO linking
COPY --from=rust-builder /output/libxg2g_transcoder.so /usr/local/lib/
RUN ldconfig

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

# Build with Rust remuxer
RUN set -eux; \
    BUILD_REF="${GIT_REF:-${VERSION:-dev}}"; \
    export GOAMD64=v2; \
    export CGO_ENABLED=1; \
    export CGO_LDFLAGS="-L/usr/local/lib -lxg2g_transcoder -lavcodec -lavformat -lavfilter -lavutil -lswresample"; \
    echo "🚀 Building xg2g with Rust remuxer for linux/amd64"; \
    go build -buildvcs=false -trimpath -tags=gpu \
    -ldflags="-s -w -X 'main.Version=${BUILD_REF}' -extldflags='-Wl,-rpath,/app/lib'" \
    -o /out/xg2g ./cmd/daemon

# =============================================================================
# Stage 4: Runtime Image - Debian Trixie with FFmpeg 7.x
# =============================================================================
FROM debian:trixie-slim AS runtime

# Install FFmpeg 7.x runtime libraries and dependencies
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg \
    libavcodec61 \
    libavformat61 \
    libavfilter10 \
    libavutil59 \
    libswresample5 \
    && rm -rf /var/lib/apt/lists/* && \
    groupadd -g 65532 xg2g && \
    useradd -u 65532 -g xg2g -d /app -s /bin/false xg2g && \
    usermod -aG video xg2g && \
    (getent group render || groupadd -r render) && \
    usermod -aG render xg2g && \
    mkdir -p /data /app/lib && \
    chown -R xg2g:xg2g /data /app

WORKDIR /app

# Copy Go daemon (dynamically linked with Rust library)
COPY --from=go-builder /out/xg2g .

# Copy Rust remuxer library
COPY --from=rust-builder /output/libxg2g_transcoder.so ./lib/

# Set library path for Rust remuxer
ENV LD_LIBRARY_PATH=/app/lib

# Cache busting: BUILD_REVISION changes on every commit
ARG BUILD_REVISION=unknown
RUN chmod +x /app/xg2g && \
    chown -R xg2g:xg2g /app && \
    echo "Build: ${BUILD_REVISION}" > /app/.build-info

VOLUME ["/data"]

# Expose API port (8080) and Stream Proxy port (18000)
EXPOSE 8080 18000

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget -qO- http://localhost:8080/api/status || exit 1

# Default configuration
ENV XG2G_DATA=/data \
    XG2G_LISTEN=:8080 \
    XG2G_OWI_BASE=http://192.168.1.100 \
    XG2G_BOUQUET=Favourites \
    XG2G_FUZZY_MAX=2

# Image metadata
LABEL org.opencontainers.image.revision="${BUILD_REVISION}" \
    org.opencontainers.image.source="https://github.com/ManuGH/xg2g" \
    org.opencontainers.image.description="Enigma2 to IPTV Gateway with Rust-powered audio transcoding"

# Run as non-root user (best practice)
# NOTE: If running Docker inside LXC (Proxmox) and experiencing permission issues
# with unprivileged ports, override with: docker run --user root ...
# or in docker-compose.yml: user: "0:0"
USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
