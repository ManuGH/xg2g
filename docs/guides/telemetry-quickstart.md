# OpenTelemetry Quick Start Guide

Get started with distributed tracing in xg2g in 5 minutes.

## Step 1: Start Jaeger

```bash
# Start Jaeger using Docker Compose
docker-compose -f docker-compose.jaeger.yml up -d

# Verify Jaeger is running
curl http://localhost:16686
```

Expected output: Jaeger UI homepage

## Step 2: Configure xg2g

### Option A: Environment Variables

```bash
export XG2G_TELEMETRY_ENABLED=true
export XG2G_SERVICE_NAME=xg2g
export XG2G_TELEMETRY_EXPORTER=grpc
export XG2G_OTLP_ENDPOINT=localhost:4317
export XG2G_SAMPLING_RATE=1.0
```

### Option B: Configuration File

```bash
# Copy example config
cp config/xg2g.example-telemetry.yaml xg2g.yaml

# Edit configuration
vim xg2g.yaml
```

Ensure telemetry section is configured:

```yaml
telemetry:
  enabled: true
  service_name: xg2g
  exporter_type: grpc
  endpoint: localhost:4317
  sampling_rate: 1.0
```

## Step 3: Start xg2g

```bash
# Build xg2g
go build -o xg2g ./cmd/daemon

# Run with telemetry
./xg2g --config xg2g.yaml
```

## Step 4: Generate Traces

### Test HTTP Endpoints

```bash
# Status endpoint
curl http://localhost:8080/api/v1/status

# Trigger EPG refresh
curl -X POST http://localhost:8080/api/v1/refresh
```

### Test GPU Transcoding (if enabled)

```bash
# Stream a channel (will create transcode traces)
curl http://localhost:8080/stream/channel123.ts
```

## Step 5: View Traces in Jaeger

1. **Open Jaeger UI**: http://localhost:16686

2. **Select Service**: Choose "xg2g" from the dropdown

3. **Click "Find Traces"**

4. **Explore Traces**:
   - Click on a trace to see detailed spans
   - Look for:
     - `GET /api/v1/status` - HTTP request spans
     - `transcode.gpu` - GPU transcoding spans
     - `job.refresh` - EPG refresh job spans

## Example Traces

### HTTP Request Trace

You should see traces like:

```
xg2g.api: GET /api/v1/status
├─ http.method: GET
├─ http.status_code: 200
├─ http.route: /api/v1/status
└─ Duration: 3.2ms
```

### GPU Transcoding Trace

```
xg2g.proxy: transcode.gpu
├─ transcode.codec: hevc
├─ transcode.device: vaapi
├─ transcode.gpu_enabled: true
├─ Events:
│  ├─ connecting to GPU transcoder (2ms)
│  └─ streaming transcoded output (45ms)
└─ Duration: 12.5s
```

### EPG Refresh Trace

```
xg2g.jobs: job.refresh
├─ bouquets.count: 2
├─ channels.total: 98
├─ duration_ms: 3421
├─ Events:
│  └─ fetching bouquets (8ms)
└─ Duration: 3.4s
```

## Troubleshooting

### No Traces Appearing

**Check xg2g logs**:
```bash
grep -i "telemetry\|otel" xg2g.log
```

Expected output:
```
INFO telemetry provider initialized service=xg2g version=1.0.0
```

**Verify Jaeger endpoint**:
```bash
# Test OTLP gRPC endpoint
nc -zv localhost 4317
```

Expected output:
```
Connection to localhost 4317 port [tcp/*] succeeded!
```

**Check environment variables**:
```bash
env | grep XG2G_TELEMETRY
```

Expected output:
```
XG2G_TELEMETRY_ENABLED=true
XG2G_TELEMETRY_EXPORTER=grpc
```

### Traces Missing Attributes

Ensure you're using the latest version:

```bash
git pull origin main
go build -o xg2g ./cmd/daemon
```

### High Memory Usage

Reduce sampling rate in production:

```yaml
telemetry:
  sampling_rate: 0.1  # 10% sampling
```

Or via environment variable:

```bash
export XG2G_SAMPLING_RATE=0.1
```

## Next Steps

1. **Explore Trace Details**: Click on individual spans to see attributes and events

2. **Filter Traces**: Use Jaeger's search filters to find specific operations:
   - Operation: `transcode.gpu`
   - Tags: `http.status_code=500`
   - Duration: `> 1s`

3. **Compare Traces**: Compare CPU vs GPU transcoding performance

4. **Set up Alerts**: Configure alerts based on trace data (requires Grafana + Tempo)

5. **Production Deployment**: See [docs/telemetry.md](./telemetry.md) for production setup

## Useful Jaeger Queries

### Find Slow Requests

```
Service: xg2g
Min Duration: 1s
```

### Find Errors

```
Service: xg2g
Tags: error=true
```

### Find GPU Transcoding Operations

```
Service: xg2g
Operation: transcode.gpu
```

### Find Recent EPG Refreshes

```
Service: xg2g
Operation: job.refresh
Lookback: 1h
```

## Architecture Overview

```
┌─────────────┐
│   Client    │
└──────┬──────┘
       │
       ▼
┌─────────────┐     Traces     ┌──────────────┐
│   xg2g      │ ─────────────▶ │   Jaeger     │
│             │  OTLP gRPC     │   (4317)     │
│  - HTTP API │                └──────┬───────┘
│  - Proxy    │                       │
│  - Jobs     │                       ▼
└─────────────┘                ┌──────────────┐
                               │  Jaeger UI   │
                               │  (16686)     │
                               └──────────────┘
```

## Performance Tips

1. **Use Sampling in Production**:
   - Development: 100% (`sampling_rate: 1.0`)
   - Production: 10-30% (`sampling_rate: 0.1-0.3`)

2. **Monitor Resource Usage**:
   ```bash
   # Check xg2g memory usage
   ps aux | grep xg2g
   ```

3. **Batch Span Export**:
   OpenTelemetry batches spans automatically, reducing network overhead

4. **Use gRPC Exporter**:
   gRPC is more efficient than HTTP for high-traffic scenarios

## Support

For issues or questions:
- GitHub Issues: https://github.com/ManuGH/xg2g/issues
- Documentation: [docs/telemetry.md](./telemetry.md)
