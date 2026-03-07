# Multi-Stage Dockerfile for xg2g with embedded FFmpeg 7.1.3
ARG BUILD_VERSION=v3.3.0
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown

# Stage 1: Build FFmpeg pinned version
FROM debian:trixie-slim AS ffmpeg-builder

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
COPY backend/scripts/build-ffmpeg.sh .
ENV TARGET_DIR=/opt/ffmpeg
RUN ./build-ffmpeg.sh

# Stage 2: Build WebUI
FROM node:22-slim AS webui-builder
WORKDIR /frontend/webui
COPY frontend/webui/package*.json ./
RUN npm ci
COPY frontend/webui/ ./
COPY backend/contracts/version_matrix.json ../../backend/contracts/version_matrix.json
RUN npm run build

# Stage 3: Build xg2g application
# Keep in sync with go.mod (currently requires Go 1.25.7).
FROM golang:1.25.7 AS app-builder
ARG BUILD_VERSION
ARG BUILD_COMMIT
ARG BUILD_DATE

WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN cd . && go mod download

COPY . /app
# Copy built WebUI assets to the correct location for Go embedding
COPY --from=webui-builder /frontend/webui/dist /app/backend/internal/control/http/dist

ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    cd /app/backend && go build -buildvcs=false -ldflags="-s -w -X main.version=${BUILD_VERSION} -X main.commit=${BUILD_COMMIT} -X main.buildDate=${BUILD_DATE}" -o /xg2g ./cmd/daemon

# Stage 3: Final runtime image
FROM debian:trixie-slim AS runtime
ARG BUILD_VERSION

# Set production environment defaults
ENV DEBIAN_FRONTEND=noninteractive \
    XG2G_LOG_FORMAT=json \
    FFMPEG_HOME="/opt/ffmpeg" \
    XG2G_FFMPEG_BIN="/usr/local/bin/ffmpeg" \
    XG2G_FFPROBE_BIN="/usr/local/bin/ffprobe"

# Install minimal runtime dependencies for FFmpeg and VAAPI
# Include both Intel (iHD) and AMD/older-Intel (Mesa radeonsi/i965) VA-API drivers
# so the image works regardless of GPU vendor.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    intel-media-va-driver \
    mesa-va-drivers \
    libva-drm2 \
    libva2 \
    libx264-164 \
    libx265-215 \
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
COPY --chown=root:root backend/scripts/ffmpeg-wrapper.sh /usr/local/bin/ffmpeg
COPY --chown=root:root backend/scripts/ffprobe-wrapper.sh /usr/local/bin/ffprobe
RUN chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

# Copy xg2g binary
COPY --from=app-builder --chown=xg2g:xg2g /xg2g /usr/local/bin/xg2g

# Switch to non-root user
USER 10001:10001

# Working directory for transient data
WORKDIR /var/lib/xg2g

# Expose ports (API + streaming)
EXPOSE 8088 18000

# Readiness Probe (uses the healthcheck subcommand)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["xg2g", "healthcheck", "ready"]

# OCI Metadata (Enterprise Standard)
LABEL org.opencontainers.image.title="xg2g" \
    org.opencontainers.image.description="Enterprise-grade Enigma2 to HDHomeRun proxy and DVR" \
    org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0" \
    org.opencontainers.image.vendor="ManuGH" \
    org.opencontainers.image.version="${BUILD_VERSION}" \
    org.opencontainers.image.source="https://github.com/ManuGH/xg2g"

# Entrypoint configuration
ENTRYPOINT ["xg2g"]
CMD ["daemon", "run"]
