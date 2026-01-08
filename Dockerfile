# syntax=docker/dockerfile:1.7

# =============================================================================
# xg2g - Enigma2 to IPTV Gateway
# Target: Linux amd64 only, Debian Trixie (FFmpeg 7.x)
# =============================================================================

# =============================================================================
# Stage 1: Build WebUI (React + Vite)
# =============================================================================
FROM node:24-slim AS node-builder
WORKDIR /webui

# Copy dependency manifests first for better layer caching
COPY webui/package*.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefer-offline --no-audit

# Copy source and build
COPY webui/ .
RUN npm run build

# =============================================================================
# Stage 2: Build Go Daemon
# =============================================================================
FROM golang:1.25-trixie AS go-builder

WORKDIR /src

# Copy Go module files first for better layer caching
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Copy WebUI artifacts from node-builder
COPY --from=node-builder /webui/dist ./internal/api/dist

# Build Go daemon
ARG GIT_REF
ARG VERSION
ARG BUILD_REVISION=unknown

RUN set -eux; \
    BUILD_REF="${GIT_REF:-${VERSION:-dev}}"; \
    export GOAMD64=v2; \
    export CGO_ENABLED=0; \
    echo "🚀 Building xg2g for linux/amd64"; \
    go build -buildvcs=false -trimpath \
    -ldflags="-s -w -X 'main.version=${BUILD_REF}'" \
    -o /out/xg2g ./cmd/daemon

# =============================================================================
# Stage 3: Runtime Image - Debian Trixie with FFmpeg 7.x
# =============================================================================
FROM debian:trixie-slim AS runtime

# Install FFmpeg 7.x runtime libraries and dependencies
# Using --no-install-recommends to minimize image size
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/* && \
    groupadd -g 65532 xg2g && \
    useradd -u 65532 -g xg2g -d /app -s /bin/false xg2g && \
    usermod -aG video xg2g && \
    (getent group render || groupadd -r render) && \
    usermod -aG render xg2g && \
    mkdir -p /data /app && \
    chown -R xg2g:xg2g /data /app

WORKDIR /app

# Copy Go daemon from builder stage
COPY --from=go-builder --chown=xg2g:xg2g /out/xg2g .

# Cache busting: BUILD_REVISION changes on every commit
ARG BUILD_REVISION=unknown
RUN chmod +x /app/xg2g && \
    echo "Build: ${BUILD_REVISION}" > /app/.build-info

VOLUME ["/data"]

# Expose API port (8080) and Stream Proxy port (18000)
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
    CMD wget -qO- http://localhost:8080/healthz || exit 1

# Default configuration
ENV XG2G_DATA=/data \
    XG2G_LISTEN=:8080 \
    XG2G_OWI_BASE= \
    XG2G_BOUQUET= \
    XG2G_FUZZY_MAX=2

# Image metadata
LABEL org.opencontainers.image.revision="${BUILD_REVISION}" \
    org.opencontainers.image.source="https://github.com/ManuGH/xg2g" \
    org.opencontainers.image.description="Enigma2 to IPTV Gateway with FFmpeg transcoding"

# Run as non-root user (best practice)
# NOTE: If running Docker inside LXC (Proxmox) and experiencing permission issues
# with unprivileged ports, override with: docker run --user root ...
# or in docker-compose.yml: user: "0:0"
USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
