# Tier 2: Production Docker Compose with Monitoring Stack

## Ziel
Out-of-Box Production Monitoring Setup mit Prometheus, Grafana, Jaeger

## Problem
Aktuell:
- Kein vorkonfiguriertes Monitoring Setup
- User müssen selbst Prometheus/Grafana aufsetzen
- Keine pre-configured Dashboards
- Keine Alert Rules

## Lösung
Production-Ready Docker Compose Stack mit:
- **Prometheus** (Metrics Collection)
- **Grafana** (Visualization)
- **Jaeger** (Distributed Tracing)
- **Alert Manager** (Alerting)

---

## Implementation

### 1. Production Docker Compose

**Datei:** `docker-compose.production.yml` (neu)

```yaml
# xg2g Production Monitoring Stack
# Complete observability setup with Prometheus, Grafana, Jaeger

version: '3.8'

services:
  # ============================================================================
  # xg2g Application (Mode 1: Standard)
  # ============================================================================
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    container_name: xg2g
    user: "65532:65532"
    restart: unless-stopped
    ports:
      - "8080:8080"      # HTTP API & M3U/EPG
      - "1900:1900/udp"  # SSDP for Plex/Jellyfin
      - "9090:9090"      # Prometheus metrics (internal)
    environment:
      # OpenWebIF Configuration
      - XG2G_OWI_BASE=${XG2G_OWI_BASE:-http://192.168.1.100}
      - XG2G_BOUQUET=${XG2G_BOUQUET:-Favourites}

      # Smart Stream Detection (enabled by default)
      - XG2G_SMART_STREAM_DETECTION=true

      # EPG Configuration
      - XG2G_EPG_ENABLED=true
      - XG2G_EPG_DAYS=7

      # Tier 1: Rate Limiting
      - XG2G_RATE_LIMIT_ENABLED=true
      - XG2G_RATE_LIMIT_GLOBAL_RATE=100
      - XG2G_RATE_LIMIT_PER_IP_RATE=10

      # Tier 1: GPU Metrics (if applicable)
      - XG2G_GPU_STATS_ENABLED=false

      # Tier 2: Circuit Breaker
      - XG2G_CIRCUIT_BREAKER_ENABLED=true
      - XG2G_CIRCUIT_BREAKER_FAILURE_THRESHOLD=5
      - XG2G_CIRCUIT_BREAKER_TIMEOUT=30s

      # Logging
      - XG2G_LOG_LEVEL=info
      - XG2G_LOG_FORMAT=json

      # Tracing
      - XG2G_TRACING_ENABLED=true
      - XG2G_JAEGER_ENDPOINT=http://jaeger:14268/api/traces

    volumes:
      - xg2g-data:/data
    networks:
      - monitoring
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    security_opt:
      - no-new-privileges:true
    cap_drop:
      - ALL
    cap_add:
      - NET_BIND_SERVICE

  # ============================================================================
  # Prometheus (Metrics Collection)
  # ============================================================================
  prometheus:
    image: prom/prometheus:v2.48.0
    container_name: prometheus
    restart: unless-stopped
    command:
      - '--config.file=/etc/prometheus/prometheus.yml'
      - '--storage.tsdb.path=/prometheus'
      - '--storage.tsdb.retention.time=30d'
      - '--web.console.libraries=/usr/share/prometheus/console_libraries'
      - '--web.console.templates=/usr/share/prometheus/consoles'
    ports:
      - "9091:9090"  # Prometheus UI (external)
    volumes:
      - ./deploy/monitoring/prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./deploy/monitoring/prometheus/alert-rules.yml:/etc/prometheus/alert-rules.yml:ro
      - prometheus-data:/prometheus
    networks:
      - monitoring
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:9090/-/healthy"]
      interval: 30s
      timeout: 5s
      retries: 3

  # ============================================================================
  # Grafana (Visualization)
  # ============================================================================
  grafana:
    image: grafana/grafana:10.2.2
    container_name: grafana
    restart: unless-stopped
    ports:
      - "3000:3000"
    environment:
      - GF_SECURITY_ADMIN_USER=admin
      - GF_SECURITY_ADMIN_PASSWORD=${GRAFANA_PASSWORD:-xg2g-admin}
      - GF_USERS_ALLOW_SIGN_UP=false
      - GF_AUTH_ANONYMOUS_ENABLED=false
      - GF_INSTALL_PLUGINS=grafana-piechart-panel
    volumes:
      - ./deploy/monitoring/grafana/provisioning:/etc/grafana/provisioning:ro
      - ./deploy/monitoring/grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    networks:
      - monitoring
    depends_on:
      - prometheus
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:3000/api/health"]
      interval: 30s
      timeout: 5s
      retries: 3

  # ============================================================================
  # Jaeger (Distributed Tracing)
  # ============================================================================
  jaeger:
    image: jaegertracing/all-in-one:1.51
    container_name: jaeger
    restart: unless-stopped
    ports:
      - "5775:5775/udp"
      - "6831:6831/udp"
      - "6832:6832/udp"
      - "5778:5778"
      - "16686:16686"  # Jaeger UI
      - "14268:14268"  # Jaeger collector
      - "14250:14250"
      - "9411:9411"
    environment:
      - COLLECTOR_ZIPKIN_HOST_PORT=:9411
      - SPAN_STORAGE_TYPE=badger
      - BADGER_EPHEMERAL=false
      - BADGER_DIRECTORY_VALUE=/badger/data
      - BADGER_DIRECTORY_KEY=/badger/key
    volumes:
      - jaeger-data:/badger
    networks:
      - monitoring
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:14269/"]
      interval: 30s
      timeout: 5s
      retries: 3

  # ============================================================================
  # Alert Manager (Optional - Alerting)
  # ============================================================================
  alertmanager:
    image: prom/alertmanager:v0.26.0
    container_name: alertmanager
    restart: unless-stopped
    ports:
      - "9093:9093"
    command:
      - '--config.file=/etc/alertmanager/alertmanager.yml'
      - '--storage.path=/alertmanager'
    volumes:
      - ./deploy/monitoring/alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
      - alertmanager-data:/alertmanager
    networks:
      - monitoring
    healthcheck:
      test: ["CMD", "wget", "-q", "-O-", "http://localhost:9093/-/healthy"]
      interval: 30s
      timeout: 5s
      retries: 3

# ============================================================================
# Volumes
# ============================================================================
volumes:
  xg2g-data:
  prometheus-data:
  grafana-data:
  jaeger-data:
  alertmanager-data:

# ============================================================================
# Networks
# ============================================================================
networks:
  monitoring:
    driver: bridge
```

---

### 2. Prometheus Configuration

**Datei:** `deploy/monitoring/prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 15s
  external_labels:
    cluster: 'xg2g-production'
    environment: 'production'

# Alerting configuration
alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

# Load rules
rule_files:
  - 'alert-rules.yml'

# Scrape configurations
scrape_configs:
  # xg2g application metrics
  - job_name: 'xg2g'
    static_configs:
      - targets: ['xg2g:9090']
        labels:
          app: 'xg2g'
          mode: 'standard'

  # Prometheus self-monitoring
  - job_name: 'prometheus'
    static_configs:
      - targets: ['localhost:9090']

  # Grafana metrics (optional)
  - job_name: 'grafana'
    static_configs:
      - targets: ['grafana:3000']
```

---

### 3. Alert Rules

**Datei:** `deploy/monitoring/prometheus/alert-rules.yml`

```yaml
groups:
  - name: xg2g_alerts
    interval: 30s
    rules:
      # High error rate
      - alert: HighErrorRate
        expr: rate(xg2g_openwebif_request_failures_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High error rate detected"
          description: "{{ $value }} errors per second (5m average)"

      # Rate limiting triggered frequently
      - alert: FrequentRateLimiting
        expr: rate(xg2g_ratelimit_exceeded_total[5m]) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Rate limiting triggered frequently"
          description: "{{ $value }} rate limit rejections per second"

      # Circuit breaker open
      - alert: CircuitBreakerOpen
        expr: xg2g_circuit_breaker_state > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "Circuit breaker {{ $labels.name }} is open"
          description: "Port {{ $labels.name }} has failed repeatedly"

      # GPU queue full
      - alert: GPUQueueFull
        expr: xg2g_gpu_queue_size > 90
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "GPU queue nearly full"
          description: "Queue size: {{ $value }} (limit: 100)"

      # High GPU utilization
      - alert: HighGPUUtilization
        expr: xg2g_gpu_utilization_percent > 85
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "GPU utilization high"
          description: "GPU {{ $labels.device }} at {{ $value }}%"

      # Service down
      - alert: ServiceDown
        expr: up{job="xg2g"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "xg2g service is down"
          description: "xg2g has been down for more than 1 minute"
```

---

### 4. Grafana Provisioning

**Datei:** `deploy/monitoring/grafana/provisioning/datasources/prometheus.yml`

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: true
```

**Datei:** `deploy/monitoring/grafana/provisioning/dashboards/default.yml`

```yaml
apiVersion: 1

providers:
  - name: 'xg2g'
    orgId: 1
    folder: ''
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /var/lib/grafana/dashboards
```

---

### 5. Grafana Dashboard

**Datei:** `deploy/monitoring/grafana/dashboards/xg2g-overview.json`

```json
{
  "dashboard": {
    "title": "xg2g Production Overview",
    "uid": "xg2g-overview",
    "tags": ["xg2g", "production"],
    "timezone": "browser",
    "schemaVersion": 27,
    "version": 1,
    "refresh": "10s",

    "panels": [
      {
        "id": 1,
        "title": "Request Rate",
        "type": "graph",
        "gridPos": {"x": 0, "y": 0, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "rate(xg2g_openwebif_request_success_total[5m])",
            "legendFormat": "Success"
          },
          {
            "expr": "rate(xg2g_openwebif_request_failures_total[5m])",
            "legendFormat": "Failures"
          }
        ]
      },
      {
        "id": 2,
        "title": "Active Streams",
        "type": "graph",
        "gridPos": {"x": 12, "y": 0, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "xg2g_active_streams_by_mode",
            "legendFormat": "{{ mode }}"
          }
        ]
      },
      {
        "id": 3,
        "title": "Rate Limiting",
        "type": "graph",
        "gridPos": {"x": 0, "y": 8, "w": 12, "h": 8},
        "targets": [
          {
            "expr": "rate(xg2g_ratelimit_exceeded_total[5m])",
            "legendFormat": "{{ limit_type }}"
          }
        ]
      },
      {
        "id": 4,
        "title": "GPU Utilization",
        "type": "gauge",
        "gridPos": {"x": 12, "y": 8, "w": 6, "h": 8},
        "targets": [
          {
            "expr": "xg2g_gpu_utilization_percent"
          }
        ],
        "fieldConfig": {
          "defaults": {
            "min": 0,
            "max": 100,
            "unit": "percent"
          }
        }
      },
      {
        "id": 5,
        "title": "Circuit Breaker States",
        "type": "stat",
        "gridPos": {"x": 18, "y": 8, "w": 6, "h": 8},
        "targets": [
          {
            "expr": "xg2g_circuit_breaker_state",
            "legendFormat": "{{ name }}"
          }
        ]
      }
    ]
  }
}
```

---

### 6. AlertManager Configuration

**Datei:** `deploy/monitoring/alertmanager/alertmanager.yml`

```yaml
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'default'

receivers:
  - name: 'default'
    # Email configuration (optional)
    # email_configs:
    #   - to: 'ops@example.com'
    #     from: 'alertmanager@example.com'
    #     smarthost: 'smtp.gmail.com:587'
    #     auth_username: 'alertmanager@example.com'
    #     auth_password: 'password'

    # Slack configuration (example)
    # slack_configs:
    #   - api_url: 'https://hooks.slack.com/services/YOUR/WEBHOOK/URL'
    #     channel: '#alerts'
    #     title: '{{ .GroupLabels.alertname }}'
    #     text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'

    # Webhook configuration (generic)
    webhook_configs:
      - url: 'http://localhost:5001/webhook'
        send_resolved: true

inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'cluster']
```

---

## Usage

### Start the Stack

```bash
# 1. Set environment variables
export XG2G_OWI_BASE=http://192.168.1.100
export GRAFANA_PASSWORD=my-secret-password

# 2. Start all services
docker-compose -f docker-compose.production.yml up -d

# 3. Verify health
docker-compose -f docker-compose.production.yml ps
```

### Access UIs

```bash
# xg2g API & Playlist
http://localhost:8080

# Prometheus
http://localhost:9091

# Grafana (admin / xg2g-admin)
http://localhost:3000

# Jaeger Tracing UI
http://localhost:16686

# AlertManager
http://localhost:9093
```

### Verify Metrics

```bash
# Check xg2g metrics
curl http://localhost:9091/api/v1/query?query=xg2g_active_streams_by_mode

# Check Prometheus targets
curl http://localhost:9091/api/v1/targets
```

---

## Environment Variables

```bash
# Required
XG2G_OWI_BASE=http://192.168.1.100
XG2G_BOUQUET=Favourites

# Optional
GRAFANA_PASSWORD=my-secret-password
```

---

## Success Criteria

- ✅ All services healthy (`docker ps`)
- ✅ Prometheus scraping xg2g (`/targets`)
- ✅ Grafana shows metrics
- ✅ Jaeger shows traces
- ✅ Alerts firing when appropriate

---

## Rollout

1. ✅ Docker Compose file
2. ✅ Prometheus config & alerts
3. ✅ Grafana provisioning & dashboard
4. ✅ AlertManager config
5. ✅ Documentation

**Effort:** 4h
