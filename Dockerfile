# Multi-Stage Dockerfile for xg2g with embedded FFmpeg 8.1
ARG BUILD_VERSION=v3.10.0
ARG BUILD_COMMIT=unknown
ARG BUILD_DATE=unknown
ARG FFMPEG_BASE_IMAGE=ffmpeg-runtime-base-internal

# Stage 1: Build FFmpeg pinned version
FROM debian:trixie-slim AS ffmpeg-builder

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential \
    curl \
    ca-certificates \
    git \
    yasm \
    nasm \
    pkg-config \
    libx264-dev \
    libx265-dev \
    libva-dev \
    libdav1d-dev \
    && rm -rf /var/lib/apt/lists/*

# Build FFmpeg
WORKDIR /build
COPY backend/scripts/build-ffmpeg.sh .
ENV TARGET_DIR=/opt/ffmpeg
RUN ./build-ffmpeg.sh

# Stage 1b: Runtime base with FFmpeg and VAAPI dependencies.
FROM debian:trixie-slim AS ffmpeg-runtime-base-internal

ARG TARGETARCH

# Set production runtime defaults shared by release and local base images.
ENV DEBIAN_FRONTEND=noninteractive \
    XG2G_LISTEN=":8088" \
    FFMPEG_HOME="/opt/ffmpeg" \
    XG2G_FFMPEG_BIN="/usr/local/bin/ffmpeg" \
    XG2G_FFPROBE_BIN="/usr/local/bin/ffprobe"

# Install minimal runtime dependencies for FFmpeg and VAAPI.
# Intel iHD userspace is only shipped for x86_64-class Debian targets, so keep
# the multi-arch truth explicit: amd64 gets Intel + Mesa; arm64 keeps the
# generic VAAPI libs and Mesa path without a fake Intel package dependency.
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    mesa-va-drivers \
    libva-drm2 \
    libva2 \
    libx264-164 \
    libx265-215 \
    libdav1d7 \
    && if [ "${TARGETARCH}" = "amd64" ]; then \
        apt-get install -y --no-install-recommends intel-media-va-driver; \
      fi \
    && apt-get upgrade -y \
    && rm -rf /var/lib/apt/lists/*

# Create runtime user and writable directories in the reusable base layer.
RUN groupadd -g 10001 xg2g && \
    useradd -u 10001 -g xg2g -m -s /sbin/nologin xg2g && \
    (getent group video || groupadd -g 44 video) && \
    usermod -aG video xg2g && \
    mkdir -p /var/lib/xg2g/recordings /var/lib/xg2g/tmp /var/lib/xg2g/sessions /etc/xg2g && \
    chown -R 10001:10001 /var/lib/xg2g /etc/xg2g

# Copy FFmpeg and wrappers into the reusable runtime base.
COPY --from=ffmpeg-builder --chown=root:root /opt/ffmpeg /opt/ffmpeg
COPY --chown=root:root backend/scripts/ffmpeg-wrapper.sh /usr/local/bin/ffmpeg
COPY --chown=root:root backend/scripts/ffprobe-wrapper.sh /usr/local/bin/ffprobe
RUN chmod +x /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

# Stage 2: Build WebUI
FROM node:26-slim AS webui-builder
WORKDIR /apps/webui
COPY apps/webui/package*.json ./
RUN npm ci
COPY apps/webui/ ./
COPY backend/contracts/version_matrix.json ../../backend/contracts/version_matrix.json
RUN npm run build

# Stage 3: Build xg2g application
# Keep in sync with go.mod (currently requires Go 1.26.5).
FROM golang:1.26.5 AS app-builder
ARG BUILD_VERSION
ARG BUILD_COMMIT
ARG BUILD_DATE

WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN cd . && go mod download

COPY . /app
# Copy built WebUI assets to the correct location for Go embedding
COPY --from=webui-builder /apps/webui/dist /app/backend/internal/control/http/dist

# Declared without values on purpose. BuildKit fills these in from the platform
# being built; giving them defaults replaces that with the default, so a
# --platform=linux/arm64 build compiled the daemon for amd64 and put it in an
# arm64 image. That stayed invisible while CGO_ENABLED and GOARCH were being
# dropped by the shell - Go then built for whatever the emulated builder was,
# which happened to be right.
ARG TARGETOS
ARG TARGETARCH
# The environment has to be on the command it is meant for. As a prefix to the
# whole line it applied to `cd` and nothing else, so `go build` ran with cgo
# enabled and the image has been shipping a dynamically linked daemon.
WORKDIR /app/backend
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -buildvcs=false -ldflags="-s -w -X github.com/ManuGH/xg2g/internal/version.Version=${BUILD_VERSION} -X github.com/ManuGH/xg2g/internal/version.Commit=${BUILD_COMMIT} -X github.com/ManuGH/xg2g/internal/version.Date=${BUILD_DATE}" -o /xg2g ./cmd/daemon

# Stage 3b: the media facts core.
#
# A separate artifact, not a Go dependency. It is built by its own toolchain into
# its own binary, and the Go build above is untouched by it - in particular that
# build stays CGO_ENABLED=0, because nothing here is ever linked into the daemon.
# Like the Go stage, this runs on the target platform, so the compile is native
# rather than cross.
FROM rust:1.91.0-slim-trixie AS media-core-builder

WORKDIR /media-core
# rust-toolchain.toml is deliberately not copied. Inside this stage the FROM tag
# is the pin; the file would become an override on top of it and send rustup to
# the network mid-build for components this stage never uses - rustfmt, clippy,
# and a foreign-arch std, none of which a native `cargo build` touches. The two
# pins are kept in step by a CI check rather than by carrying the file here.
COPY media-core/Cargo.toml media-core/Cargo.lock ./
COPY media-core/src ./src

# --locked so a build that would have to change Cargo.lock fails here instead of
# silently resolving to something nobody reviewed.
RUN cargo build --release --locked && \
    install -m 0755 target/release/xg2g-media-core /xg2g-media-core

# Stage 4: Final runtime image.
# By default this inherits the internal FFmpeg base stage.
# Local/CI fast paths can override FFMPEG_BASE_IMAGE with a prebuilt base image tag.
ARG FFMPEG_BASE_IMAGE=ffmpeg-runtime-base-internal
FROM ${FFMPEG_BASE_IMAGE} AS runtime
ARG BUILD_VERSION

# Copy xg2g binary
COPY --from=app-builder --chown=10001:10001 /xg2g /usr/local/bin/xg2g

# Copy the media facts core. Present in the image, started by nobody: the daemon
# does not launch it yet, and its runtime semantics are unchanged by its presence.
COPY --from=media-core-builder --chown=10001:10001 /xg2g-media-core /usr/local/bin/xg2g-media-core

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
