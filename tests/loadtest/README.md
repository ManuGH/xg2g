# xg2g Load Testing

Performance and load testing for xg2g using various tools.

## Tools

- **hey**: Simple HTTP load generator
- **k6**: Modern load testing tool with scripting
- **vegeta**: HTTP load testing tool with constant throughput

## Prerequisites

### Install hey
```bash
# macOS
brew install hey

# Go install
go install github.com/rakyll/hey@latest
```

### Install k6
```bash
# macOS
brew install k6

# Linux
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

### Install vegeta
```bash
# macOS
brew install vegeta

# Go install
go install github.com/tsenart/vegeta@latest
```

## Running Tests

### Quick Load Test (hey)

Test API endpoints:
```bash
./loadtest-hey.sh
```

### Comprehensive Load Test (k6)

```bash
# Run default test
k6 run loadtest-k6.js

# Run with custom VUs and duration
k6 run --vus 50 --duration 5m loadtest-k6.js

# Run with stages
k6 run --stage 1m:10,5m:50,1m:10 loadtest-k6.js
```

### Constant Throughput Test (vegeta)

```bash
# 100 requests/second for 30 seconds
echo "GET http://localhost:8080/api/health" | vegeta attack -rate 100 -duration 30s | vegeta report

# With multiple endpoints
./loadtest-vegeta.sh
```

## Test Scenarios

### 1. Health Check Baseline
```bash
hey -n 10000 -c 100 http://localhost:8080/healthz
```

### 2. API Endpoints
```bash
hey -n 5000 -c 50 http://localhost:8080/api/channels
```

### 3. M3U Playlist
```bash
hey -n 1000 -c 20 http://localhost:8080/playlist.m3u
```

### 4. EPG XML
```bash
hey -n 500 -c 10 http://localhost:8080/epg.xml
```

### 5. Streaming Endpoint
```bash
# Simulate multiple concurrent streams
hey -n 100 -c 10 -t 30 http://localhost:8080/stream/<service_ref>
```

## Interpreting Results

### Key Metrics

- **Requests/sec**: Throughput (higher is better)
- **Latency**: Response time distribution
  - p50: Median response time
  - p95: 95th percentile (most users experience this or better)
  - p99: 99th percentile (edge case performance)
- **Success rate**: % of non-error responses (should be 100%)
- **Errors**: Count and types of errors

### Expected Performance

| Endpoint | Target RPS | p95 Latency | Notes |
|----------|-----------|-------------|-------|
| /healthz | 1000+ | <10ms | Lightweight check |
| /api/channels | 100+ | <100ms | Cached response |
| /playlist.m3u | 50+ | <200ms | Medium payload |
| /epg.xml | 20+ | <500ms | Large XML payload |
| /stream/* | 50+ | <100ms | Proxy setup time |

## Stress Testing

### CPU Stress Test
```bash
# Ramp up to high load
k6 run --vus 1 --duration 1m \
       --vus 10 --duration 2m \
       --vus 50 --duration 2m \
       --vus 100 --duration 2m \
       --vus 200 --duration 2m \
       loadtest-k6.js
```

### Memory Stress Test
```bash
# Sustained high load
k6 run --vus 100 --duration 30m loadtest-k6.js
```

### Auto-Scaling Validation
```bash
# Test HPA scaling behavior
k6 run --vus 1 --duration 2m \
       --vus 50 --duration 5m \
       --vus 1 --duration 3m \
       loadtest-k6.js

# Watch pod scaling
watch kubectl get hpa -n xg2g
```

## Monitoring During Tests

### Prometheus Queries

```promql
# Request rate
rate(http_requests_total[1m])

# Latency p95
histogram_quantile(0.95, rate(http_request_duration_seconds_bucket[5m]))

# Error rate
rate(http_requests_total{code=~"5.."}[1m]) / rate(http_requests_total[1m])

# Active connections
xg2g_active_streams_total

# GPU queue size
xg2g_gpu_queue_size
```

### Grafana Dashboard

Open Grafana during load test:
```bash
# Port-forward if running in Kubernetes
kubectl port-forward -n monitoring svc/grafana 3000:3000

# Open in browser
open http://localhost:3000
```

### System Metrics

Monitor system resources:
```bash
# Docker Compose
docker stats xg2g

# Kubernetes
kubectl top pods -n xg2g
kubectl top nodes
```

## Troubleshooting

### High Error Rate

Check logs:
```bash
# Docker Compose
docker logs xg2g -f --tail 100

# Kubernetes
kubectl logs -n xg2g -l app=xg2g -f --tail=100
```

### High Latency

1. Check CPU/Memory usage
2. Check database/Redis latency
3. Check OpenWebIF endpoint health
4. Review Prometheus metrics for bottlenecks

### Connection Refused

Ensure service is running:
```bash
# Docker Compose
docker-compose ps

# Kubernetes
kubectl get pods -n xg2g
kubectl get svc -n xg2g
```

## Best Practices

1. **Start small**: Begin with low load and gradually increase
2. **Warm up**: Run a short warm-up test before main test
3. **Monitor**: Watch metrics during test execution
4. **Isolate**: Test one endpoint at a time to identify bottlenecks
5. **Realistic**: Use realistic request patterns (mixed endpoints, think time)
6. **Production-like**: Test against production-like environment
7. **Baseline**: Establish baseline performance before changes

## Results Storage

Save results for comparison:
```bash
# hey
hey -n 10000 -c 100 http://localhost:8080/healthz > results/baseline-healthz.txt

# k6
k6 run --out json=results/k6-$(date +%Y%m%d-%H%M%S).json loadtest-k6.js

# vegeta
vegeta attack -rate 100 -duration 30s < targets.txt | vegeta report > results/vegeta-report.txt
```

## Continuous Load Testing

Integrate into CI/CD:
```yaml
# .github/workflows/loadtest.yml
- name: Run Load Tests
  run: |
    k6 run --vus 10 --duration 1m --thresholds 'http_req_duration{p(95)}<200' loadtest-k6.js
```
