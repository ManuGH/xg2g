# Multi-Stage Dockerfile for xg2g with embedded FFmpeg 7.1.3
# Stage 1: Build FFmpeg (Debian-based for compatibility)
FROM debian:bookworm-slim AS ffmpeg-builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    ca-certificates \
    yasm \
    nasm \
    pkg-config \
    libx264-dev \
    libx265-dev \
    libva-dev \
    && rm -rf /var/lib/apt/lists/*

# Build FFmpeg
WORKDIR /build
COPY scripts/build-ffmpeg.sh .
ENV TARGET_DIR=/opt/ffmpeg
RUN ./build-ffmpeg.sh

# Stage 2: Build xg2g application
FROM golang:1.23-bookworm AS app-builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /xg2g ./cmd/daemon

# Stage 3: Runtime image (minimal)
FROM debian:bookworm-slim

# Install runtime dependencies for FFmpeg
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libx264-164 \
    libx265-199 \
    libva2 \
    libva-drm2 \
    intel-media-va-driver \
    && rm -rf /var/lib/apt/lists/*

# Copy FFmpeg from builder
COPY --from=ffmpeg-builder /opt/ffmpeg /opt/ffmpeg

# Install FFmpeg wrappers (scoped LD_LIBRARY_PATH, no global leak)
COPY scripts/ffmpeg-wrapper.sh /usr/local/bin/ffmpeg
COPY scripts/ffprobe-wrapper.sh /usr/local/bin/ffprobe
RUN chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

# Set FFMPEG_HOME for wrappers (optional, wrappers have defaults)
ENV FFMPEG_HOME="/opt/ffmpeg"

# Set xg2g FFmpeg paths to use wrappers (explicit contract)
ENV XG2G_FFMPEG_PATH="/usr/local/bin/ffmpeg"

# Copy xg2g binary
COPY --from=app-builder /xg2g /usr/local/bin/xg2g

# Expose ports (API + streaming)
EXPOSE 8088 18000

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
    CMD ["/usr/local/bin/xg2g", "--version"] || exit 1

ENTRYPOINT ["/usr/local/bin/xg2g"]
