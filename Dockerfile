FROM golang:1.22-alpine AS builder

# Build-Umgebung
WORKDIR /src

# Dependencies zuerst für besseres Caching
COPY go.mod go.sum ./
RUN go mod download

# Quellcode kopieren
COPY . .

# Statisch linked binary bauen
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X 'main.Version=$(git describe --tags 2>/dev/null || echo dev)'" \
    -o /out/xg2g ./cmd/daemon

# Production Image
FROM alpine:3.20

# Metadaten
LABEL org.opencontainers.image.title="xg2g" \
      org.opencontainers.image.description="OpenWebIF to M3U/XMLTV Proxy" \
      org.opencontainers.image.url="https://github.com/ManuGH/xg2g" \
      org.opencontainers.image.source="https://github.com/ManuGH/xg2g"

# Non-root User erstellen
RUN adduser -D -H -s /sbin/nologin -u 1000 app && \
    apk add --no-cache \
        ca-certificates \
        tzdata \
        curl

# Auf non-root User wechseln
USER app
WORKDIR /app

# Binary aus Build-Stage kopieren
COPY --from=builder --chown=app:app /out/xg2g .

# Healthcheck (mit curl statt wget - besser in Alpine)
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:34400/api/status || exit 1

# Expose Port
EXPOSE 34400

# Data Volume
VOLUME ["/data"]

# Umgebungsvariablen mit Defaults
ENV XG2G_DATA=/data \
    XG2G_LISTEN=:34400 \
    XG2G_OWI_BASE=http://10.10.55.57 \
    XG2G_BOUQUET=Premium \
    XG2G_FUZZY_MAX=2

# Entrypoint
ENTRYPOINT ["/app/xg2g"]
