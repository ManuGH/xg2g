FROM golang:1.22-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.Version=1.0.0" \
    -o /out/xg2g ./cmd/daemon

FROM alpine:3.20
LABEL org.opencontainers.image.title="xg2g" \
      org.opencontainers.image.description="OpenWebIF to M3U/XMLTV Proxy" \
      org.opencontainers.image.url="https://github.com/ManuGH/xg2g" \
      org.opencontainers.image.source="https://github.com/ManuGH/xg2g"

RUN adduser -D -H -s /sbin/nologin -u 1000 app && \
    apk add --no-cache ca-certificates tzdata curl

USER app
WORKDIR /app
COPY --from=builder --chown=app:app /out/xg2g .

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -f http://localhost:34400/api/status || exit 1

EXPOSE 34400
VOLUME ["/data"]

ENV XG2G_DATA=/data \
    XG2G_LISTEN=:34400 \
    XG2G_OWI_BASE=http://10.10.55.57 \
    XG2G_BOUQUET=Premium \
    XG2G_FUZZY_MAX=2

ENTRYPOINT ["/app/xg2g"]
