# xg2g Hybrid Architecture: Go + Rust Integration

## Executive Summary

This document describes the hybrid Go + Rust architecture for xg2g, combining Go's strengths in API handling and orchestration with Rust's performance for stream processing and audio transcoding.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        xg2g System                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  ┌──────────────────────────────────────────────────────┐  │
│  │           Go Component (xg2g-daemon)                 │  │
│  ├──────────────────────────────────────────────────────┤  │
│  │  • HTTP API Server (port 8080)                      │  │
│  │  • OpenWebIF Client & Channel Management            │  │
│  │  • EPG Processing & XMLTV Generation                │  │
│  │  • HDHomeRun Emulation & SSDP Discovery             │  │
│  │  • M3U/M3U8 Playlist Generation                     │  │
│  │  • Configuration Management                          │  │
│  │  • Metrics & Telemetry (Prometheus)                 │  │
│  │  • Service Orchestration                             │  │
│  └────────────────┬─────────────────────────────────────┘  │
│                   │                                         │
│                   │ HTTP/gRPC                               │
│                   │                                         │
│  ┌────────────────▼─────────────────────────────────────┐  │
│  │       Rust Component (xg2g-transcoder)              │  │
│  ├──────────────────────────────────────────────────────┤  │
│  │  • Native MPEG-TS Stream Processing                 │  │
│  │  • Audio Remuxing (MP2/AC3 → AAC)                  │  │
│  │  • Hardware-Accelerated Transcoding (VAAPI)         │  │
│  │  • Zero-Copy Stream Buffering                       │  │
│  │  • Low-Latency Audio Pipeline                       │  │
│  │  • Performance-Critical Operations                   │  │
│  └──────────────────────────────────────────────────────┘  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

### Go Component (xg2g-daemon)

**Strengths:**
- Excellent HTTP server and networking stack
- Great concurrency model (goroutines)
- Easy JSON/XML handling
- Mature ecosystem for APIs
- Fast compilation and deployment

**Responsibilities:**
1. **API Layer**
   - RESTful API endpoints
   - Authentication & authorization
   - Request routing and validation

2. **Business Logic**
   - Channel management
   - EPG data processing
   - Playlist generation
   - Configuration management

3. **Integration Layer**
   - OpenWebIF client
   - HDHomeRun emulation
   - SSDP discovery

4. **Orchestration**
   - Service lifecycle management
   - Health checks
   - Metrics collection

### Rust Component (xg2g-transcoder)

**Strengths:**
- Zero-cost abstractions
- Memory safety without garbage collection
- Predictable performance
- Excellent for systems programming
- No runtime overhead

**Responsibilities:**
1. **Stream Processing**
   - MPEG-TS packet parsing
   - Stream demuxing and remuxing
   - Buffer management (zero-copy where possible)

2. **Audio Processing**
   - Native MP2/AC3 decoding
   - AAC encoding
   - Audio format conversion
   - Low-latency pipeline

3. **Hardware Acceleration**
   - VAAPI integration
   - GPU-accelerated transcoding
   - Hardware codec access

4. **Performance-Critical Operations**
   - Real-time stream manipulation
   - High-throughput data processing
   - Latency-sensitive operations

## Integration Patterns

### Pattern 1: HTTP API Communication (Current)

```
Go Proxy → HTTP GET → Rust Transcoder → Stream Response
```

**Pros:**
- Simple to implement
- Language-agnostic
- Easy to debug and monitor

**Cons:**
- HTTP overhead
- Not optimal for very high throughput

**Use Case:** Current GPU transcoding implementation

### Pattern 2: Unix Domain Sockets (Recommended)

```
Go Proxy → UDS → Rust Transcoder → Stream Response
```

**Pros:**
- Lower latency than HTTP
- No network stack overhead
- Still process-isolated

**Cons:**
- Slightly more complex setup

**Use Case:** Audio-only remuxing (latency-critical)

### Pattern 3: Shared Memory (Future)

```
Go Proxy ←→ Shared Memory ←→ Rust Transcoder
```

**Pros:**
- Absolute minimum latency
- True zero-copy possible

**Cons:**
- Most complex to implement
- Requires careful synchronization

**Use Case:** Future ultra-low-latency optimization

## Audio Remuxing Pipeline

### Current Implementation (Go + ffmpeg)

```
Receiver Stream → Go Proxy → ffmpeg process → Output
                              ↓
                         Latency: 200-500ms
                         CPU: Moderate
                         Sync: Issues on iOS
```

### Proposed Implementation (Go + Rust Native)

```
Receiver Stream → Go Proxy → Rust Transcoder → Output
                              ↓
                         • Native MPEG-TS parser
                         • Symphonia MP2/AC3 decoder
                         • fdk-aac encoder
                         • Native MPEG-TS muxer
                         ↓
                         Latency: <50ms
                         CPU: Low (hardware-assisted)
                         Sync: Perfect
```

## Technology Stack

### Rust Libraries

```toml
[dependencies]
# MPEG-TS Processing
mpeg2ts-reader = "0.17"  # 54k downloads, production-ready

# Audio Decoding
symphonia = { version = "0.5", features = ["mp2", "aac"] }
symphonia-codec-aac = "0.5"

# Audio Encoding
fdk-aac = "0.6"  # High-quality AAC encoder

# Async Runtime
tokio = { version = "1", features = ["full"] }

# HTTP Server
axum = "0.8"

# Metrics
metrics = "0.24"
metrics-exporter-prometheus = "0.16"

# Logging
tracing = "0.1"
tracing-subscriber = "0.3"
```

### Go Integration

```go
// internal/transcoder/rust_client.go
package transcoder

type RustTranscoder struct {
    baseURL string
    client  *http.Client
}

func (t *RustTranscoder) TranscodeAudio(sourceURL string) (io.ReadCloser, error) {
    url := fmt.Sprintf("%s/audio-remux?source=%s", t.baseURL, url.QueryEscape(sourceURL))
    resp, err := t.client.Get(url)
    if err != nil {
        return nil, err
    }
    return resp.Body, nil
}
```

## Deployment Architecture

### Docker Compose Setup

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    ports:
      - "8080:8080"
    environment:
      - XG2G_TRANSCODER_URL=http://xg2g-transcoder:3000
    depends_on:
      - xg2g-transcoder

  xg2g-transcoder:
    image: ghcr.io/manugh/xg2g-transcoder:latest
    ports:
      - "3000:3000"
    devices:
      - /dev/dri:/dev/dri  # VAAPI support
    environment:
      - RUST_LOG=info
      - VAAPI_DEVICE=/dev/dri/renderD128
```

### Multi-Stage Build

```dockerfile
# Stage 1: Build Rust transcoder
FROM rust:1.75 AS rust-builder
WORKDIR /build
COPY transcoder/ .
RUN cargo build --release

# Stage 2: Build Go daemon
FROM golang:1.25 AS go-builder
WORKDIR /build
COPY . .
RUN CGO_ENABLED=0 go build -o xg2g ./cmd/daemon

# Stage 3: Runtime
FROM alpine:3.22
RUN apk add --no-cache ca-certificates ffmpeg
COPY --from=rust-builder /build/target/release/xg2g-transcoder /usr/local/bin/
COPY --from=go-builder /build/xg2g /usr/local/bin/
```

## Implementation Phases

### Phase 1: Foundation (Week 1-2)
- ✅ Document architecture
- ✅ Set up Rust transcoder project structure
- ✅ Implement basic HTTP API in Rust
- ✅ Add Go client for Rust transcoder

### Phase 2: Native Audio Remuxing (Week 3-4)
- 🔄 Implement MPEG-TS parser with mpeg2ts-reader
- 🔄 Add MP2/AC3 decoder with Symphonia
- 🔄 Integrate AAC encoder (fdk-aac)
- 🔄 Build MPEG-TS muxer

### Phase 3: Integration (Week 5-6)
- ⏳ Replace ffmpeg audio transcoding with Rust service
- ⏳ Add Unix Domain Socket support
- ⏳ Implement connection pooling
- ⏳ Add comprehensive error handling

### Phase 4: Optimization (Week 7-8)
- ⏳ Profile and optimize hot paths
- ⏳ Implement zero-copy optimizations
- ⏳ Add hardware-accelerated audio decoding
- ⏳ Performance benchmarking

### Phase 5: Production (Week 9-10)
- ⏳ Comprehensive testing
- ⏳ Documentation
- ⏳ Deployment automation
- ⏳ Monitoring and alerting

## Performance Goals

| Metric | Current (ffmpeg) | Target (Native Rust) |
|--------|------------------|----------------------|
| Audio Latency | 200-500ms | <50ms |
| CPU Usage | 15-20% | <5% |
| Memory Usage | 80-100MB | <30MB |
| Throughput | 50 Mbps | 200+ Mbps |
| iOS Sync | ❌ Broken | ✅ Perfect |

## Testing Strategy

### Unit Tests
- Go: Standard `go test`
- Rust: `cargo test`

### Integration Tests
- Docker Compose test environment
- Automated stream validation
- Audio sync verification

### Performance Tests
- Load testing with multiple concurrent streams
- Latency measurements
- Resource usage profiling

## Monitoring & Observability

### Metrics (Prometheus)
```
# Go metrics
xg2g_http_requests_total
xg2g_stream_active_count
xg2g_epg_refresh_duration_seconds

# Rust metrics
xg2g_transcoder_audio_remux_duration_seconds
xg2g_transcoder_active_streams
xg2g_transcoder_bytes_processed_total
```

### Logging
- Go: zerolog (JSON structured logging)
- Rust: tracing (JSON structured logging)
- Unified log aggregation (e.g., Loki)

### Tracing
- OpenTelemetry support
- Distributed tracing across Go ↔ Rust boundary

## Security Considerations

1. **Process Isolation**
   - Rust transcoder runs in separate container
   - Limited privileges
   - No direct filesystem access

2. **Input Validation**
   - Go validates all external inputs
   - Rust validates stream format

3. **Resource Limits**
   - CPU/memory limits in Docker
   - Connection limits
   - Rate limiting

## Migration Strategy

### Backward Compatibility
- Keep existing ffmpeg-based transcoding as fallback
- Feature flag for Rust transcoder: `XG2G_USE_RUST_TRANSCODER=true`
- Gradual rollout

### Rollback Plan
- Easy toggle back to ffmpeg
- Monitor error rates
- Automated rollback on failures

## Future Enhancements

1. **Native Video Transcoding**
   - Extend to video codec handling
   - Full hardware-accelerated pipeline

2. **Advanced Audio Processing**
   - Loudness normalization
   - Audio enhancement
   - Multi-language support

3. **Adaptive Streaming**
   - HLS segmentation in Rust
   - ABR (Adaptive Bitrate) support

4. **Edge Computing**
   - Deploy Rust transcoder closer to users
   - CDN integration

## Conclusion

This hybrid architecture leverages the best of both worlds:
- **Go** for stable, maintainable API and business logic
- **Rust** for performance-critical stream processing

The result is a system that combines:
- ✅ Maintainability (Go)
- ✅ Performance (Rust)
- ✅ Safety (both languages)
- ✅ Scalability (microservices)

## References

- [Rust MPEG-TS Reader](https://github.com/dholroyd/mpeg2ts-reader)
- [Symphonia Audio Library](https://github.com/pdeljanov/Symphonia)
- [Go-Rust Integration Patterns](https://blog.golang.org/cgo)
- [VAAPI Documentation](https://www.freedesktop.org/wiki/Software/vaapi/)

---

**Status:** Phase 1 Complete, Phase 2 In Progress
**Last Updated:** 2025-10-29
**Author:** xg2g Team
