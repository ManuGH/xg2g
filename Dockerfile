# syntax=docker/dockerfile:1.7

# =============================================================================
# xg2g - Enigma2 to IPTV Gateway
# Target: Linux amd64 only, Debian Trixie (FFmpeg 7.x)
# =============================================================================

# =============================================================================
# Stage 1: Build WebUI (React + Vite)
# =============================================================================
FROM node:20-slim AS node-builder
WORKDIR /webui
COPY webui/package*.json ./
RUN npm ci
COPY webui/ .
RUN npm run build

# =============================================================================
# Stage 2: Build Go Daemon
# =============================================================================
FROM golang:1.25-trixie AS go-builder

WORKDIR /src

# Copy Go source
COPY go.mod go.sum ./
RUN go mod download

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
RUN apt-get update && apt-get upgrade -y && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    wget \
    ffmpeg \
    && rm -rf /var/lib/apt/lists/* && \
    groupadd -g 65532 xg2g && \
    useradd -u 65532 -g xg2g -d /app -s /bin/false xg2g && \
    usermod -aG video xg2g && \
    (getent group render || groupadd -r render) && \
    usermod -aG render xg2g && \
    mkdir -p /data /app && \
    chown -R xg2g:xg2g /data /app

WORKDIR /app

# Copy Go daemon
COPY --from=go-builder /out/xg2g .

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
