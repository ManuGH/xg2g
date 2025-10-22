# ADR-004: OpenTelemetry Integration for Distributed Tracing

**Status**: Accepted
**Date**: 2025-01-21
**Deciders**: Development Team
**Technical Story**: Priority 4 Implementation

## Context and Problem Statement

xg2g handles complex workflows:
- HTTP requests through multiple handlers and middleware
- GPU transcoding with FFmpeg or external transcoder service
- EPG refresh jobs with concurrent channel fetching
- Proxy streaming with potential buffering/transcoding

Without distributed tracing, debugging performance issues and bottlenecks is difficult:
- "Why is GPU transcoding slow?" → No visibility into FFmpeg or transcoder latency
- "Which EPG channels are slowest?" → No per-channel timing data
- "Where do requests spend time?" → No request flow visualization

## Decision Drivers

- **Performance Debugging**: Identify bottlenecks in GPU transcoding pipeline
- **Production Observability**: Monitor request flows in real deployments
- **Standards Compliance**: Use industry-standard OpenTelemetry
- **Vendor Neutrality**: Work with Jaeger, Tempo, Zipkin, or any OTLP backend
- **Minimal Overhead**: <1% CPU/memory overhead with sampling

## Considered Options

1. **OpenTelemetry with OTLP Exporters**
2. **Jaeger Native Client Libraries**
3. **Zipkin Native Client**
4. **Custom Logging-Based Tracing**
5. **No Tracing** (rely on logs and metrics only)

## Decision Outcome

**Chosen option**: "OpenTelemetry with OTLP Exporters"

### Rationale

1. **Industry Standard**: OpenTelemetry is CNCF standard, widely adopted
2. **Vendor Neutral**: Works with Jaeger, Tempo, Zipkin, Datadog, New Relic, etc.
3. **Future-Proof**: OTLP protocol ensures compatibility with future backends
4. **Rich Ecosystem**: Auto-instrumentation for HTTP, gRPC, databases
5. **Context Propagation**: Automatic trace context across services

### Positive Consequences

- **Deep Visibility**: See exact time spent in GPU transcoding, EPG fetching, etc.
- **Production-Ready**: Sampling controls overhead in high-traffic scenarios
- **Multi-Backend**: Switch from Jaeger to Tempo without code changes
- **Correlation**: Link traces with metrics and logs (OpenTelemetry Collector)

### Negative Consequences

- **Complexity**: Adds OpenTelemetry SDK dependencies
- **Learning Curve**: Team must learn tracing concepts (spans, context, attributes)
- **Storage**: Traces require backend storage (Jaeger, Tempo)

## Pros and Cons of the Options

### OpenTelemetry with OTLP

- **Good**, because vendor-neutral and future-proof
- **Good**, because rich instrumentation libraries
- **Good**, because automatic context propagation
- **Bad**, because adds dependency on OpenTelemetry SDK

### Jaeger Native Client

- **Good**, because simpler than OpenTelemetry
- **Bad**, because vendor lock-in (Jaeger-specific)
- **Bad**, because can't switch to Tempo/Zipkin without code changes

### Custom Logging-Based Tracing

- **Good**, because no external dependencies
- **Bad**, because no standard format or tooling
- **Bad**, because manual correlation of logs is error-prone

### No Tracing

- **Good**, because simplest (no changes)
- **Bad**, because debugging performance issues is guesswork
- **Bad**, because no request flow visualization

## Implementation

### Architecture

```
┌─────────────────────┐
│   xg2g (Go)         │
│  ┌───────────────┐  │
│  │ HTTP Handler  │  │ ← Start span
│  │   ├─ Proxy    │  │ ← Child span
│  │   └─ GPU      │  │ ← Child span
│  └───────────────┘  │
│         │           │
│    OTLP Exporter    │
└─────────┬───────────┘
          │ gRPC/HTTP
          ↓
┌─────────────────────┐
│  Jaeger / Tempo     │ ← Trace backend
└─────────────────────┘
```

### Instrumented Components

**1. HTTP Middleware** (`internal/api/middleware/tracing.go`):
```go
func Tracing(tracerName string) func(http.Handler) http.Handler {
    tracer := telemetry.Tracer(tracerName)
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path)
            defer span.End()

            span.SetAttributes(telemetry.HTTPAttributes(
                r.Method, r.URL.Path, r.URL.String(), 0,
            )...)

            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

**2. GPU Transcoding** (`internal/proxy/transcoder.go`):
```go
func (t *Transcoder) ProxyToGPUTranscoder(ctx context.Context, ...) error {
    tracer := telemetry.Tracer("xg2g.proxy")
    ctx, span := tracer.Start(ctx, "transcode.gpu")
    defer span.End()

    span.SetAttributes(
        attribute.String("transcode.codec", "hevc"),
        attribute.Bool("transcode.gpu_enabled", true),
    )

    span.AddEvent("connecting to GPU transcoder")
    // ... transcoding logic
    span.AddEvent("streaming transcoded output")
}
```

**3. EPG Refresh Job** (`internal/jobs/refresh.go`):
```go
func Refresh(ctx context.Context, cfg Config) (*Status, error) {
    tracer := telemetry.Tracer("xg2g.jobs")
    ctx, span := tracer.Start(ctx, "job.refresh")
    defer span.End()

    startTime := time.Now()

    span.AddEvent("fetching bouquets")
    // ... job logic

    duration := time.Since(startTime)
    span.SetAttributes(
        attribute.Int("channels.total", status.Channels),
        attribute.Int64("duration_ms", duration.Milliseconds()),
    )
}
```

### Configuration

**Environment Variables**:
```bash
export XG2G_TELEMETRY_ENABLED=true
export XG2G_SERVICE_NAME=xg2g
export XG2G_TELEMETRY_EXPORTER=grpc    # or "http"
export XG2G_OTLP_ENDPOINT=localhost:4317
export XG2G_SAMPLING_RATE=1.0          # 100% for dev, 0.1 for prod
```

**YAML Config**:
```yaml
telemetry:
  enabled: true
  service_name: xg2g
  service_version: 1.0.0
  environment: production
  exporter_type: grpc
  endpoint: tempo:4317
  sampling_rate: 0.1  # 10% sampling in production
```

### Example Traces

**GPU Transcoding Trace**:
```
xg2g.proxy: transcode.gpu (12.5s)
├─ transcode.codec: hevc
├─ transcode.device: vaapi
├─ transcode.gpu_enabled: true
├─ Events:
│  ├─ connecting to GPU transcoder (2ms)
│  └─ streaming transcoded output (45ms)
└─ Duration: 12.5s
```

**EPG Refresh Trace**:
```
xg2g.jobs: job.refresh (4.5s)
├─ bouquets.count: 3
├─ channels.total: 145
├─ Events:
│  └─ fetching bouquets (10ms)
└─ Duration: 4.5s
```

### Sampling Strategy

**Development**: 100% sampling
```go
sampling_rate: 1.0
```

**Production**: 10-30% sampling
```go
sampling_rate: 0.1  // 10% of requests
```

**Dynamic Sampling** (Future):
```go
// Sample all errors, 10% of success
if err != nil {
    sampler = trace.AlwaysSample()
} else {
    sampler = trace.TraceIDRatioBased(0.1)
}
```

## Local Development Setup

**Docker Compose** (`docker-compose.jaeger.yml`):
```yaml
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"  # Jaeger UI
      - "4317:4317"    # OTLP gRPC
      - "4318:4318"    # OTLP HTTP
```

**Start Jaeger**:
```bash
docker-compose -f docker-compose.jaeger.yml up -d
```

**Access Jaeger UI**: http://localhost:16686

## Testing Strategy

**Unit Tests**:
```go
func TestTracing_Success(t *testing.T) {
    // Setup noop tracer for tests
    _, err := telemetry.NewProvider(context.Background(), telemetry.Config{
        Enabled: false,
    })

    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        span := trace.SpanFromContext(r.Context())
        if span == nil {
            t.Error("Expected span in context")
        }
    })

    tracedHandler := middleware.Tracing("test")(handler)
    // ... test
}
```

## Migration Path

**Phase 1 (Current)**: Foundation
- ✅ Telemetry package created
- ✅ HTTP middleware implemented
- ✅ GPU transcoding instrumented
- ✅ EPG refresh instrumented
- ✅ Jaeger Docker Compose setup

**Phase 2 (Future)**: Enhanced Instrumentation
- Database queries (if added)
- External API calls (OpenWebIF)
- Cache hits/misses
- Queue operations

**Phase 3 (Advanced)**: Metrics Correlation
- Link traces with Prometheus metrics
- Exemplars (trace IDs in metrics)
- Unified observability (logs + metrics + traces)

## Links

- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [OTLP Specification](https://opentelemetry.io/docs/reference/specification/protocol/otlp/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- Implementation: `internal/telemetry/`
- Documentation: `docs/telemetry.md`, `docs/telemetry-quickstart.md`
- Related: ADR-001 (API Versioning) - version-aware tracing

## Notes

### Lessons Learned

1. **Noop Provider for Tests**: Disable tracing in tests to avoid test pollution
2. **Span Events Are Valuable**: Mark milestones (e.g., "connecting to GPU")
3. **Context Propagation**: Always pass `context.Context` through call chains
4. **Sampling Is Critical**: 100% sampling in production kills performance

### Performance Impact

- **CPU Overhead**: <1% with 10% sampling
- **Memory**: ~10MB for tracer provider
- **Network**: ~1KB per span (compressed)

### Future Considerations

1. **Trace-Based Alerting**: Alert on slow traces (>5s GPU transcoding)
2. **Automatic Instrumentation**: Use OpenTelemetry auto-instrumentation
3. **Baggage Propagation**: Propagate user context across services
4. **Tail-Based Sampling**: Keep all traces with errors, sample successes

### Common Pitfalls

**Pitfall 1**: Forgetting to `defer span.End()`
- **Solution**: Always use `defer span.End()` immediately after `Start()`

**Pitfall 2**: High cardinality attributes (e.g., user IDs)
- **Solution**: Limit attribute values to low-cardinality enums

**Pitfall 3**: Missing context propagation
- **Solution**: Pass `context.Context` through all function calls
