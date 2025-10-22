# OpenTelemetry Tracing

This document describes the OpenTelemetry distributed tracing implementation in xg2g.

## Overview

xg2g uses OpenTelemetry for distributed tracing, providing deep insights into:
- HTTP request handling
- GPU transcoding performance
- EPG refresh job execution
- End-to-end request flows

## Architecture

### Tracer Provider

The telemetry package (`internal/telemetry`) provides a centralized tracer provider that supports:

- **Exporters**: OTLP gRPC and HTTP exporters for Jaeger/Tempo/other backends
- **Sampling**: Configurable sampling rates (0.0 to 1.0)
- **Resource Detection**: Automatic service name, version, and environment tagging

### Instrumented Components

| Component | Tracer Name | Span Types |
|-----------|-------------|------------|
| HTTP API | `xg2g.api` | `GET /api/v1/*` |
| Proxy/Transcoding | `xg2g.proxy` | `transcode.cpu`, `transcode.gpu` |
| Jobs | `xg2g.jobs` | `job.refresh` |

## Configuration

### Environment Variables

```bash
# Enable telemetry
export XG2G_TELEMETRY_ENABLED=true

# Service identification
export XG2G_SERVICE_NAME=xg2g
export XG2G_SERVICE_VERSION=1.0.0
export XG2G_ENVIRONMENT=production

# Exporter configuration
export XG2G_TELEMETRY_EXPORTER=grpc  # or "http"
export XG2G_OTLP_ENDPOINT=localhost:4317  # gRPC endpoint

# Sampling rate (0.0 to 1.0, where 1.0 = 100%)
export XG2G_SAMPLING_RATE=1.0
```

### Configuration File (YAML)

```yaml
telemetry:
  enabled: true
  service_name: xg2g
  service_version: 1.0.0
  environment: production
  exporter_type: grpc
  endpoint: localhost:4317
  sampling_rate: 1.0
```

## Local Development with Jaeger

### Starting Jaeger

Use the provided Docker Compose configuration:

```bash
docker-compose -f docker-compose.jaeger.yml up -d
```

This starts Jaeger with:
- **UI**: http://localhost:16686
- **OTLP gRPC**: localhost:4317
- **OTLP HTTP**: localhost:4318

### Running xg2g with Tracing

```bash
# Set environment variables
export XG2G_TELEMETRY_ENABLED=true
export XG2G_TELEMETRY_EXPORTER=grpc
export XG2G_OTLP_ENDPOINT=localhost:4317
export XG2G_SAMPLING_RATE=1.0

# Start xg2g
./xg2g
```

### Viewing Traces

1. Open Jaeger UI: http://localhost:16686
2. Select service: `xg2g`
3. Click "Find Traces"

## Trace Examples

### HTTP Request Trace

```
xg2g.api: GET /api/v1/status
├─ Attributes:
│  ├─ http.method: GET
│  ├─ http.route: /api/v1/status
│  ├─ http.status_code: 200
│  └─ http.url: http://localhost:8080/api/v1/status
└─ Duration: 5ms
```

### GPU Transcoding Trace

```
xg2g.proxy: transcode.gpu
├─ Attributes:
│  ├─ transcode.codec: hevc
│  ├─ transcode.device: vaapi
│  ├─ transcode.gpu_enabled: true
│  └─ transcoder.url: http://localhost:8085
├─ Events:
│  ├─ connecting to GPU transcoder (t=2ms)
│  └─ streaming transcoded output (t=50ms)
└─ Duration: 15432ms
```

### EPG Refresh Job Trace

```
xg2g.jobs: job.refresh
├─ Attributes:
│  ├─ bouquets.count: 3
│  ├─ channels.total: 145
│  └─ duration_ms: 4523
├─ Events:
│  └─ fetching bouquets (t=10ms)
└─ Duration: 4523ms
```

## Span Attributes Reference

### HTTP Attributes

| Attribute | Type | Example |
|-----------|------|---------|
| `http.method` | string | `GET` |
| `http.status_code` | int | `200` |
| `http.route` | string | `/api/v1/status` |
| `http.url` | string | `http://localhost:8080/api/v1/status` |

### Transcoding Attributes

| Attribute | Type | Example |
|-----------|------|---------|
| `transcode.codec` | string | `hevc` |
| `transcode.input_codec` | string | `h264` |
| `transcode.output_codec` | string | `hevc` |
| `transcode.device` | string | `vaapi` |
| `transcode.gpu_enabled` | bool | `true` |
| `transcode.bitrate` | int | `4000000` |

### EPG Attributes

| Attribute | Type | Example |
|-----------|------|---------|
| `epg.days` | int | `7` |
| `epg.channels` | int | `100` |
| `epg.events` | int | `1500` |
| `epg.concurrency` | int | `5` |

### Job Attributes

| Attribute | Type | Example |
|-----------|------|---------|
| `job.type` | string | `epg-refresh` |
| `job.status` | string | `completed` |
| `job.duration_ms` | int64 | `4523` |
| `bouquets.count` | int | `3` |
| `channels.total` | int | `145` |

## Production Deployment

### Jaeger Backend

For production, deploy Jaeger with persistent storage:

```yaml
# docker-compose.prod.yml
services:
  jaeger:
    image: jaegertracing/all-in-one:latest
    environment:
      - SPAN_STORAGE_TYPE=elasticsearch
      - ES_SERVER_URLS=http://elasticsearch:9200
    ports:
      - "4317:4317"  # OTLP gRPC
      - "16686:16686"  # UI
```

### Grafana Tempo

Alternatively, use Grafana Tempo for scalable tracing:

```yaml
# docker-compose.tempo.yml
services:
  tempo:
    image: grafana/tempo:latest
    command: ["-config.file=/etc/tempo.yaml"]
    ports:
      - "4317:4317"  # OTLP gRPC
      - "3200:3200"  # Tempo HTTP
```

Configure xg2g:

```bash
export XG2G_TELEMETRY_ENABLED=true
export XG2G_TELEMETRY_EXPORTER=grpc
export XG2G_OTLP_ENDPOINT=tempo:4317
export XG2G_SAMPLING_RATE=0.1  # 10% sampling for production
```

## Performance Impact

### Sampling Recommendations

| Environment | Sampling Rate | Reason |
|-------------|---------------|--------|
| Development | 1.0 (100%) | Full visibility |
| Staging | 0.5 (50%) | High visibility, lower overhead |
| Production (low traffic) | 1.0 (100%) | Acceptable overhead |
| Production (high traffic) | 0.1-0.3 (10-30%) | Balance visibility/performance |

### Overhead

OpenTelemetry adds minimal overhead:
- **CPU**: < 1% with 100% sampling
- **Memory**: ~10MB for tracer provider
- **Network**: ~1KB per span

## Troubleshooting

### No Traces in Jaeger

1. **Check telemetry is enabled**:
   ```bash
   curl http://localhost:8080/api/v1/status
   ```
   Look for `telemetry.enabled: true` in response.

2. **Verify OTLP endpoint**:
   ```bash
   # Test gRPC endpoint
   grpcurl -plaintext localhost:4317 list
   ```

3. **Check logs**:
   ```bash
   # Look for telemetry initialization errors
   grep -i "telemetry\|otel" xg2g.log
   ```

### High Memory Usage

Reduce sampling rate:

```bash
export XG2G_SAMPLING_RATE=0.1  # 10% sampling
```

### Missing Attributes

Ensure you're using the latest version of the tracer:

```go
import "github.com/ManuGH/xg2g/internal/telemetry"

tracer := telemetry.Tracer("my-component")
```

## Best Practices

### 1. Use Descriptive Span Names

```go
// Good
ctx, span := tracer.Start(ctx, "transcode.gpu")

// Bad
ctx, span := tracer.Start(ctx, "process")
```

### 2. Add Relevant Attributes

```go
span.SetAttributes(
    attribute.String("codec", "hevc"),
    attribute.Int("bitrate", 4000000),
)
```

### 3. Record Errors Properly

```go
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, "operation failed")
    return err
}
```

### 4. Use Events for Milestones

```go
span.AddEvent("fetching source stream")
// ... do work ...
span.AddEvent("streaming transcoded output")
```

### 5. Always End Spans

```go
ctx, span := tracer.Start(ctx, "operation")
defer span.End()  // Ensures span is always ended
```

## Integration with Other Tools

### Prometheus Metrics

Combine tracing with Prometheus metrics for complete observability:

```go
import (
    "github.com/ManuGH/xg2g/internal/metrics"
    "github.com/ManuGH/xg2g/internal/telemetry"
)

// Record both metric and trace
metrics.IncTranscodeRequests("gpu")
ctx, span := telemetry.Tracer("proxy").Start(ctx, "transcode.gpu")
defer span.End()
```

### Grafana Dashboards

Create dashboards combining:
- Trace data from Tempo/Jaeger
- Metrics from Prometheus
- Logs from Loki

Example query:
```promql
# Show P95 latency for GPU transcoding
histogram_quantile(0.95,
  rate(xg2g_transcode_duration_seconds_bucket{type="gpu"}[5m])
)
```

## References

- [OpenTelemetry Go Documentation](https://opentelemetry.io/docs/instrumentation/go/)
- [Jaeger Documentation](https://www.jaegertracing.io/docs/)
- [Grafana Tempo Documentation](https://grafana.com/docs/tempo/latest/)
- [OTLP Specification](https://opentelemetry.io/docs/reference/specification/protocol/otlp/)
