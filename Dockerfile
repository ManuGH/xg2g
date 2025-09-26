FROM golang:1.22-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux \
    go build -buildvcs=false -trimpath \
      -ldflags="-s -w -X 'main.Version=${GIT_REF:-dev}'" \
      -o /out/xg2g ./cmd/daemon

FROM alpine:3.20
RUN adduser -D -H -s /sbin/nologin -u 1000 app && \
    apk add --no-cache ca-certificates tzdata curl
WORKDIR /app
COPY --from=builder /out/xg2g /app/xg2g
USER app
EXPOSE 34400
ENTRYPOINT ["/app/xg2g"]
