# xg2g Production Improvements - Implementation Roadmap

## Overview

Dieser Roadmap definiert konkrete Verbesserungen für xg2g basierend auf der Code-Review und Production-Anforderungen.

## Status Quo (✅ Already Solid)

### Observability
- ✅ Prometheus Metrics (Request Duration, Retries, Failures)
- ✅ OpenTelemetry Tracing (Jaeger Integration)
- ✅ Structured Logging (Zerolog, JSON output)
- ✅ Health & Readiness Probes (`/healthz`, `/readyz`)

### Performance
- ✅ HTTP Connection Pooling (MaxIdle: 100, MaxPerHost: 20)
- ✅ Configurable via ENV variables

### Security
- ✅ API Token Authentication (constant-time comparison)
- ✅ OIDC Integration (planned, documented)

### Testing
- ✅ 79.5% Code Coverage (Codecov)
- ✅ Race Detector in CI
- ✅ Integration Tests
- ✅ Security Scans (gosec, trivy)

---

## Tier 1: Production Critical (Sprint 1-2)

### 1.1 GPU Metrics (Mode 3 Observability)

**Problem:** Keine Sichtbarkeit über GPU-Auslastung, VRAM, Transcode-Latency

**Lösung:**
- Neue Prometheus Metrics: `xg2g_gpu_utilization_percent`, `xg2g_transcode_latency_seconds`, `xg2g_vram_usage_bytes`
- Integration in `internal/proxy/transcoder.go`
- Grafana Dashboard für GPU Monitoring

**Files:**
- `internal/metrics/gpu.go` (neu)
- `internal/proxy/transcoder.go` (erweitern)
- `deploy/monitoring/grafana/dashboards/gpu-transcoding.json` (neu)

**Effort:** 6-8h

**Details:** [TIER1_GPU_METRICS.md](TIER1_GPU_METRICS.md)

---

### 1.2 Rate Limiting Middleware

**Problem:** Kein Schutz vor Überlastung durch zu viele parallele Requests

**Lösung:**
- Per-IP, Per-Mode, Global Rate Limiting
- HTTP 429 Too Many Requests mit `Retry-After` Header
- Metrics: `xg2g_rate_limit_exceeded_total`

**Files:**
- `internal/ratelimit/limiter.go` (neu)
- `internal/api/middleware/ratelimit.go` (neu)
- `internal/api/http.go` (erweitern)

**Effort:** 4-6h

**Details:** [TIER1_RATE_LIMITING.md](TIER1_RATE_LIMITING.md)

---

### 1.3 Stream Detection Error Metrics

**Problem:** Keine Visibility über Stream Detection Fehler (Port 8001/17999)

**Lösung:**
- Counter: `xg2g_stream_detection_errors_total{port, error_type}`
- Integration in `internal/openwebif/stream_detection.go`

**Files:**
- `internal/metrics/gpu.go` (erweitern, siehe 1.1)
- `internal/openwebif/stream_detection.go` (erweitern)

**Effort:** 2h

---

## Tier 2: High-Value Features (Sprint 3-4)

### 2.1 Circuit Breaker für Stream Detection

**Problem:** Bei wiederholten Fehlern wird Stream Detection weiter versucht → Latency

**Lösung:**
- Circuit Breaker mit 3 States: Closed, Open, Half-Open
- Automatisches Fallback auf alternativen Port

**Files:**
- `internal/openwebif/circuit_breaker.go` (neu)
- `internal/openwebif/stream_detection.go` (erweitern)

**Effort:** 4h

---

### 2.2 GPU Queue System (Mode 3 Multi-Client)

**Problem:** Bei >10 parallelen GPU Streams kann GPU überlastet werden

**Lösung:**
- Priority Queue: Active (Priority 2), Normal (Priority 1), Preview (Priority 0)
- Configurable Queue Size und Worker Count

**Files:**
- `internal/proxy/gpu_queue.go` (neu)
- `internal/proxy/transcoder.go` (erweitern)

**Effort:** 6h

---

### 2.3 Production Docker Compose mit Monitoring Stack

**Problem:** Kein Out-of-Box Monitoring Setup für Production

**Lösung:**
- Docker Compose mit Prometheus, Grafana, Jaeger
- Pre-configured Dashboards
- Alert Rules

**Files:**
- `docker-compose.production.yml` (neu)
- `deploy/monitoring/prometheus/prometheus.yml` (erweitern)
- `deploy/monitoring/grafana/dashboards/*.json` (neu)

**Effort:** 4h

---

## Tier 3: Nice-to-Have / Langfristig (Sprint 5+)

### 3.1 Safari/iOS AAC Validation Tests

**Problem:** Keine automatischen Tests für Safari/iOS Kompatibilität

**Lösung:**
- Integration Tests mit User-Agent Simulation
- ADTS Header Validation

**Files:**
- `test/integration/safari_test.go` (neu)

**Effort:** 4h

---

### 3.2 Adaptive Bitrate Streaming (HLS/DASH)

**Problem:** Single-Bitrate Stream → schlechte UX bei langsamer Verbindung

**Lösung:**
- Multi-Bitrate HLS Manifest (1080p, 720p, 480p)
- Requires FFmpeg Segmenting

**Files:**
- `internal/proxy/hls_muxer.go` (neu)

**Effort:** 12h+

---

### 3.3 Cloudflare Tunnel Integration

**Problem:** Port-Forwarding für externe Zugriffe unsicher

**Lösung:**
- Docker Sidecar mit cloudflared
- Zero-Trust Networking

**Files:**
- `docker-compose.cloudflare.yml` (neu)
- `docs/CLOUDFLARE_TUNNEL.md` (neu)

**Effort:** 3h

---

## Sprint Plan

### Sprint 1 (Woche 1-2): Foundation
- ✅ GPU Metrics (1.1) - 8h
- ✅ Rate Limiting (1.2) - 6h
- ✅ Stream Detection Metrics (1.3) - 2h
- **Total:** 16h

**Deliverables:**
- Neue Prometheus Metrics exportiert
- Grafana Dashboard für GPU
- Rate Limiting aktiv in Production

---

### Sprint 2 (Woche 3-4): Stability
- ✅ Circuit Breaker (2.1) - 4h
- ✅ GPU Queue System (2.2) - 6h
- ✅ Production Compose (2.3) - 4h
- **Total:** 14h

**Deliverables:**
- Stream Detection robuster
- GPU Multi-Client Support
- Production Monitoring Stack

---

### Sprint 3+ (Optional): Polish
- Safari Tests (3.1) - 4h
- HLS/DASH (3.2) - 12h+
- Cloudflare Tunnel (3.3) - 3h

---

## Environment Variables (Neue)

```bash
# GPU Metrics (Optional - requires tools)
XG2G_GPU_STATS_ENABLED=true
XG2G_GPU_STATS_INTERVAL=5s

# Rate Limiting
XG2G_RATE_LIMIT_ENABLED=true
XG2G_RATE_LIMIT_GLOBAL_RATE=100
XG2G_RATE_LIMIT_GLOBAL_BURST=200
XG2G_RATE_LIMIT_PER_IP_RATE=10
XG2G_RATE_LIMIT_PER_IP_BURST=20
XG2G_RATE_LIMIT_MODE_STANDARD=50
XG2G_RATE_LIMIT_MODE_AUDIO_PROXY=30
XG2G_RATE_LIMIT_MODE_GPU=20

# Circuit Breaker
XG2G_CIRCUIT_BREAKER_THRESHOLD=5
XG2G_CIRCUIT_BREAKER_TIMEOUT=30s
XG2G_CIRCUIT_BREAKER_SUCCESS_THRESHOLD=3

# GPU Queue
XG2G_GPU_QUEUE_SIZE=100
XG2G_GPU_QUEUE_WORKERS=4
```

---

## Success Metrics

### Tier 1 Success Criteria:
- ✅ GPU utilization < 85% bei 10 parallelen Streams
- ✅ P95 transcode latency < 50ms
- ✅ Rate-Limited Requests < 1% aller Requests
- ✅ Stream Detection Errors < 0.5%

### Tier 2 Success Criteria:
- ✅ Circuit Breaker verhindert Cascading Failures
- ✅ GPU Queue hält Latency < 100ms bei 20+ Clients
- ✅ Production Compose läuft out-of-box

---

## Testing Strategy

### Tier 1:
- Unit Tests für alle neuen Packages (>90% Coverage)
- Integration Tests mit Mock OpenWebIF
- Load Tests mit 50+ parallelen Requests

### Tier 2:
- Circuit Breaker State Transition Tests
- GPU Queue Priority Tests
- End-to-End Monitoring Stack Test

---

## Rollout Plan

### Phase 1: Development (Woche 1-2)
1. Implement Tier 1 Features
2. Write Tests
3. Update Documentation

### Phase 2: Testing (Woche 3)
1. Integration Tests
2. Load Tests
3. Security Audit

### Phase 3: Production (Woche 4)
1. Deploy to Staging
2. Monitor Metrics
3. Gradual Rollout to Production

---

## Documentation Updates

- ✅ Update `README.md` mit neuen Features
- ✅ Add `docs/RATE_LIMITING.md`
- ✅ Add `docs/GPU_MONITORING.md`
- ✅ Update `config.example.yaml` mit neuen ENV vars

---

## Dependencies

### Go Modules (neu):
```go
golang.org/x/time v0.5.0 // Rate limiting
```

### External Tools (optional):
- `intel_gpu_top` (Intel GPU stats)
- `radeontop` (AMD GPU stats)
- `nvidia-smi` (NVIDIA GPU stats)

---

## Review Checkpoints

### After Sprint 1:
- [ ] Metrics visible in Grafana?
- [ ] Rate limiting works für alle 3 Modi?
- [ ] No performance degradation?

### After Sprint 2:
- [ ] Circuit breaker verhindert failures?
- [ ] GPU queue handles 20+ clients?
- [ ] Production compose deployable?

---

## Open Questions

1. **GPU Stats Collection:** Welche GPU hast du? (Intel/AMD/NVIDIA)
   - Intel: `intel_gpu_top` verwenden
   - AMD: `radeontop` oder sysfs
   - NVIDIA: `nvidia-smi`

2. **Rate Limits:** Sind die Defaults (100 global, 10 per IP) passend für dein Setup?

3. **Monitoring Stack:** Willst du Prometheus/Grafana im gleichen Compose oder separates Setup?

---

## Contact & Feedback

Fragen oder Feedback? Erstelle ein Issue oder PR!
