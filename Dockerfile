FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG GIT_REF
RUN CGO_ENABLED=0 GOOS=linux \
  go build -buildvcs=false -trimpath \
  -ldflags="-s -w -X 'main.Version=${GIT_REF:-dev}'" \
  -o /out/xg2g ./cmd/daemon

FROM alpine:3.20.1
RUN adduser -D -H -s /sbin/nologin -u 1000 app && \
  apk add --no-cache ca-certificates tzdata wget
USER app
WORKDIR /app
COPY --from=builder --chown=app:app /out/xg2g .
VOLUME ["/data"]
EXPOSE 34400
HEALTHCHECK --interval=30s --timeout=10s --start-period=40s --retries=3 \
  CMD wget -qO- http://localhost:34400/api/status || exit 1
ENV XG2G_DATA=/data \
  XG2G_LISTEN=:34400 \
  XG2G_OWI_BASE=http://10.10.55.57 \
  XG2G_BOUQUET=Premium \
  XG2G_FUZZY_MAX=2
ENTRYPOINT ["/app/xg2g"]
