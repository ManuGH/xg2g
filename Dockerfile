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
FROM golang:1.25-bookworm AS app-builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-s -w" -o /xg2g ./cmd/daemon

# Stage 3: Final runtime image
FROM debian:bookworm-slim AS runtime

# Set production environment defaults
ENV DEBIAN_FRONTEND=noninteractive \
    XG2G_LOG_FORMAT=json \
    FFMPEG_HOME="/opt/ffmpeg" \
    XG2G_FFMPEG_BIN="/usr/local/bin/ffmpeg" \
    XG2G_FFPROBE_BIN="/usr/local/bin/ffprobe"

# Install minimal runtime dependencies for FFmpeg and VAAPI
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    intel-media-va-driver \
    libva-drm2 \
    libva2 \
    libx264-164 \
    libx265-199 \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (UID 10001 for cloud-native compatibility)
# Add to video group if it exists for VAAPI device access
RUN groupadd -g 10001 xg2g && \
    useradd -u 10001 -g xg2g -m -s /sbin/nologin xg2g && \
    (getent group video || groupadd -g 44 video) && \
    usermod -aG video xg2g

# Create necessary directories and set ownership
RUN mkdir -p /var/lib/xg2g/recordings /var/lib/xg2g/tmp /var/lib/xg2g/sessions /etc/xg2g && \
    chown -R xg2g:xg2g /var/lib/xg2g /etc/xg2g

# Copy FFmpeg from builder
COPY --from=ffmpeg-builder --chown=root:root /opt/ffmpeg /opt/ffmpeg

# Install FFmpeg wrappers (scoped LD_LIBRARY_PATH, no global leak)
COPY --chown=root:root scripts/ffmpeg-wrapper.sh /usr/local/bin/ffmpeg
COPY --chown=root:root scripts/ffprobe-wrapper.sh /usr/local/bin/ffprobe
RUN chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

# Copy xg2g binary
COPY --from=app-builder --chown=xg2g:xg2g /xg2g /usr/local/bin/xg2g

# Switch to non-root user
USER 10001:10001

# Working directory for transient data
WORKDIR /var/lib/xg2g

# Expose ports (API + streaming)
EXPOSE 8088 18000

# Health check (uses the binary itself)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
    CMD ["/usr/local/bin/xg2g", "--version"] || exit 1

# Metadata
LABEL maintainer="ManuGH" \
    version="3.1.3" \
    description="xg2g - High Performance Enigma2 to HLS/VOD Gateway" \
    security="non-root, pinned-deps, multi-stage"

# Entrypoint configuration
ENTRYPOINT ["/usr/local/bin/xg2g"]
CMD ["--config", "/etc/xg2g/config.yaml"]
