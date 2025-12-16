# Dockerfile for Rust GPU Transcoder Service
# High-performance transcoding with VAAPI hardware acceleration

FROM rust:1.84-bookworm AS builder

WORKDIR /build

# Install FFmpeg development libraries
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        pkg-config \
        libavcodec-dev \
        libavformat-dev \
        libavutil-dev \
        libavfilter-dev \
        libswscale-dev \
        libswresample-dev \
        libclang-dev \
        clang && \
    rm -rf /var/lib/apt/lists/*

# Copy Cargo files
COPY Cargo.toml ./

# Build arguments for CPU optimization
ARG RUST_TARGET_CPU=x86-64-v3
ARG RUST_OPT_LEVEL=3

# Create dummy main to cache dependencies
RUN mkdir src && \
    echo "fn main() {}" > src/main.rs && \
    RUSTFLAGS="-C target-cpu=${RUST_TARGET_CPU} -C opt-level=${RUST_OPT_LEVEL}" \
    cargo build --release && \
    rm -rf src target/release/xg2g-transcoder* target/release/.fingerprint/xg2g-transcoder-*

# Copy source code
COPY src ./src

# Build release binary with optimizations
# Default: x86-64-v3 (AVX2, FMA, BMI2) - works on Intel Haswell+ and AMD Zen+
# For your system: override with --build-arg RUST_TARGET_CPU=znver4
RUN RUSTFLAGS="-C target-cpu=${RUST_TARGET_CPU} -C opt-level=${RUST_OPT_LEVEL}" \
    cargo build --release && \
    strip /build/target/release/xg2g-transcoder

# Runtime image
FROM debian:bookworm-slim

# Install runtime dependencies
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        libavcodec59 \
        libavformat59 \
        libavutil57 \
        libavfilter8 \
        libswscale6 \
        libswresample4 \
        ffmpeg \
        vainfo \
        mesa-va-drivers \
        intel-media-va-driver && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/target/release/xg2g-transcoder .

EXPOSE 8081

ENV RUST_LOG=info \
    VAAPI_DEVICE=/dev/dri/renderD128 \
    VIDEO_BITRATE=5000k \
    AUDIO_CODEC=aac \
    AUDIO_BITRATE=192k \
    AUDIO_CHANNELS=2 \
    ANALYZE_DURATION=2000000 \
    PROBE_SIZE=2000000

ENTRYPOINT ["/app/xg2g-transcoder"]
