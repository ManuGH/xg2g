FROM golang:1.25-alpine AS builder

# Build arguments for CPU optimization
ARG GO_AMD64_LEVEL=v2
ARG GO_GCFLAGS=""

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG GIT_REF
ARG VERSION
RUN BUILD_REF="${GIT_REF:-${VERSION:-dev}}" && \
  CGO_ENABLED=0 GOOS=linux GOAMD64=${GO_AMD64_LEVEL} \
  go build -buildvcs=false -trimpath \
  -ldflags="-s -w -X 'main.Version=${BUILD_REF}'" \
  ${GO_GCFLAGS:+-gcflags="${GO_GCFLAGS}"} \
  -o /out/xg2g ./cmd/daemon

FROM alpine:3.22.2@sha256:4b7ce07002c69e8f3d704a9c5d6fd3053be500b7f1c69fc0d80990c2ad8dd412
RUN apk add --no-cache ca-certificates tzdata wget ffmpeg && \
  addgroup -g 65532 -S xg2g && \
  adduser -u 65532 -S -G xg2g -h /app -s /bin/false xg2g && \
  mkdir -p /data && \
  chown -R xg2g:xg2g /data
WORKDIR /app
COPY --from=builder /out/xg2g .
RUN chmod +x /app/xg2g && \
  chown xg2g:xg2g /app/xg2g
VOLUME ["/data"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD wget -qO- http://localhost:8080/api/status || exit 1
ENV XG2G_DATA=/data \
  XG2G_LISTEN=:8080 \
  XG2G_OWI_BASE=http://192.168.1.100 \
  XG2G_BOUQUET=Favourites \
  XG2G_FUZZY_MAX=2
USER xg2g:xg2g
ENTRYPOINT ["/app/xg2g"]
