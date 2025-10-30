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

# Copy Rust transcoder source
COPY transcoder/Cargo.toml transcoder/Cargo.lock ./
COPY transcoder/src ./src

# Build Rust remuxer library (cdylib for FFI)
# This creates libxg2g_transcoder.so that Go can load via CGO
ARG RUST_TARGET_CPU=x86-64-v2
RUN RUSTFLAGS="-C target-cpu=${RUST_TARGET_CPU} -C opt-level=3" \
    cargo build --release && \
    strip target/release/libxg2g_transcoder.so

# =============================================================================
# Stage 2: Build Go Daemon with CGO (required for Rust FFI)
# =============================================================================
FROM golang:1.25-alpine AS go-builder

# Install build dependencies for CGO
RUN apk add --no-cache \
    gcc \
    musl-dev \
    ffmpeg-dev

# Build arguments for CPU optimization
ARG GO_AMD64_LEVEL=v2
ARG GO_GCFLAGS=""

WORKDIR /src

# Copy Rust library for CGO linking
COPY --from=rust-builder /build/target/release/libxg2g_transcoder.so /usr/local/lib/
RUN ldconfig /usr/local/lib 2>/dev/null || true

# Copy Go source
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Build Go daemon WITH CGO enabled for Rust FFI bindings
ARG GIT_REF
ARG VERSION
RUN BUILD_REF="${GIT_REF:-${VERSION:-dev}}" && \
  CGO_ENABLED=1 GOOS=linux GOAMD64=${GO_AMD64_LEVEL} \
  go build -tags=gpu -buildvcs=false -trimpath \
  -ldflags="-s -w -X 'main.Version=${BUILD_REF}'" \
  ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
  -o /out/xg2g ./cmd/daemon

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

# Copy Rust remuxer library
COPY --from=rust-builder /build/target/release/libxg2g_transcoder.so ./lib/

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
