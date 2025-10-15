# Production Deployment Guide

Complete guide for deploying xg2g in production environments.

## Quick Start

```bash
# 1. Clone and prepare
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# 2. Configure
cp .env.production.example .env.production
nano .env.production

# 3. Deploy
docker-compose -f deploy/docker-compose.production.yml --env-file .env.production up -d

# 4. Verify
curl http://localhost:8080/healthz
curl http://localhost:8080/api/status
```

## Configuration

### Required Settings

```bash
XG2G_OWI_BASE=http://your-receiver-ip
XG2G_BOUQUET=YourBouquet
XG2G_API_TOKEN=your-secure-token-here
XG2G_EPG_ENABLED=true
XG2G_EPG_DAYS=7
```

### Security

Generate secure API token:
```bash
openssl rand -hex 32
```

Set in environment:
```bash
XG2G_API_TOKEN=your-generated-token
```

### Retry & Stability

Default retry configuration (adjust if needed):

```bash
XG2G_OWI_RETRIES=3              # Max retry attempts
XG2G_OWI_TIMEOUT_MS=10000       # Request timeout (10s)
XG2G_OWI_BACKOFF_MS=500         # Initial backoff delay
XG2G_OWI_MAX_BACKOFF_MS=2000    # Max backoff delay
```

Retry strategy uses exponential backoff:
- Attempt 1: immediate
- Attempt 2: 500ms delay
- Attempt 3: 1000ms delay
- Attempt 4: 2000ms delay (capped)

## Operations

### Health Checks

```bash
# Health endpoint
curl http://localhost:8080/healthz

# Status with metrics
curl http://localhost:8080/api/status

# Prometheus metrics
curl http://localhost:9090/metrics
```

### Manual Refresh

```bash
curl -X POST http://localhost:8080/api/refresh \
  -H "X-API-Token: YOUR_TOKEN"
```

### Logs

```bash
# View logs
docker-compose -f deploy/docker-compose.production.yml logs -f

# Filter by service
docker logs xg2g
```

### Restart

```bash
# Restart all
docker-compose -f deploy/docker-compose.production.yml restart

# Restart specific service
docker-compose -f deploy/docker-compose.production.yml restart xg2g
```

## Monitoring

### Prometheus Metrics

Key metrics to monitor:

- `xg2g_channels_total` - Number of channels
- `xg2g_epg_programmes_collected` - EPG programme count
- `xg2g_refresh_duration_seconds` - Refresh performance
- `xg2g_http_requests_total` - API request count

### Grafana Dashboard

See [deploy/monitoring/](../deploy/monitoring/) for setup.

## Deployment Options

### Docker Compose (Recommended)

```bash
docker-compose -f deploy/docker-compose.production.yml up -d
```

### Kubernetes

```bash
kubectl apply -f deploy/k8s-distroless.yaml
kubectl apply -f deploy/k8s-secret-simple.example.yaml
```

### Systemd

```bash
cp deploy/systemd-timer.conf /etc/systemd/system/xg2g.service
systemctl enable xg2g
systemctl start xg2g
```

## Security Hardening

### Firewall

Only expose necessary ports:
- 8080: HTTP API (use reverse proxy with TLS)
- 9090: Metrics (internal only)

### Reverse Proxy

Use nginx/traefik with TLS:

```nginx
server {
    listen 443 ssl;
    server_name xg2g.yourdomain.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8080;
        proxy_set_header Host $host;
    }
}
```

### API Token

Always use API token for refresh endpoint:
```bash
XG2G_API_TOKEN=your-secure-token
```

Rotate token regularly (see [security docs](security/token-rotation-2025-10.md)).

## Troubleshooting

### Service Won't Start

```bash
# Check logs
docker-compose -f deploy/docker-compose.production.yml logs

# Verify config
docker-compose -f deploy/docker-compose.production.yml config
```

### No EPG Data

```bash
# Check EPG collection
curl http://localhost:8080/files/xmltv.xml | grep -c '<programme'

# Force refresh
curl -X POST http://localhost:8080/api/refresh -H "X-API-Token: TOKEN"
```

### Receiver Timeout

Increase timeout and retries:
```bash
XG2G_OWI_TIMEOUT_MS=20000
XG2G_OWI_RETRIES=5
```

### Performance Issues

Check metrics:
```bash
curl http://localhost:9090/metrics | grep refresh_duration
```

Optimize EPG settings:
```bash
XG2G_EPG_MAX_CONCURRENCY=5      # Reduce concurrent requests
XG2G_EPG_TIMEOUT_MS=15000       # Increase timeout
```

## Backup & Recovery

### Backup Configuration

```bash
# Backup .env
cp .env.production .env.production.backup

# Backup data directory
tar -czf xg2g-data-backup.tar.gz ./data/
```

### Restore

```bash
# Restore config
cp .env.production.backup .env.production

# Restore data
tar -xzf xg2g-data-backup.tar.gz
```

## Further Reading

- [Advanced Configuration](ADVANCED.md) - Complete configuration reference
- [Monitoring Setup](../deploy/monitoring/) - Prometheus & Grafana
- [Security](security/SECURITY.md) - Security best practices
