# 🚀 xg2g Production Deployment Guide

## Security-Hardened Production Setup

### Prerequisites

- Docker & Docker Compose installed
- SSL certificates for HTTPS (recommended)
- Firewall configured (see Security section)

### Quick Production Start

```bash
# 1. Clone and prepare
git clone https://github.com/ManuGH/xg2g.git
cd xg2g

# 2. Configure environment
cp .env.example .env.production
# Edit .env.production with your settings

# 3. Start production stack
docker-compose -f docker-compose.production.yml --env-file .env.production up -d

# 4. Verify security
curl -I http://localhost:8080/api/status
# Should show security headers: X-Content-Type-Options, etc.
```

### Environment Configuration

Create `.env.production`:

```bash
# === Core Configuration ===
XG2G_OWI_BASE=http://your-receiver-ip
XG2G_BOUQUET=YourBouquetName
XG2G_XMLTV=true

# === Security Settings ===
GRAFANA_PASSWORD=your-secure-password-here
XG2G_OWI_TIMEOUT_MS=30000
XG2G_OWI_RETRIES=3

# === Performance Tuning ===
XG2G_FUZZY_MAX=2
XG2G_OWI_BACKOFF_MS=1000
```

### Security Checklist ✅

- [ ] **Firewall Rules**:

  ```bash
  # Allow only necessary ports
  ufw allow 22      # SSH
  ufw allow 80      # HTTP (redirect to HTTPS)
  ufw allow 443     # HTTPS
  ufw allow 3000    # Grafana (restrict to admin IPs)
  ufw deny 9090     # Block direct metrics access
  ufw deny 9091     # Block direct Prometheus access
  ```

- [ ] **TLS/HTTPS Setup**:

  ```bash
  # Use Traefik or nginx for SSL termination
  # Example Traefik labels in docker-compose.production.yml
  ```

- [ ] **Network Segmentation**:
  - xg2g in isolated Docker network
  - Monitoring stack in separate network
  - No direct internet access to metrics ports

- [ ] **Log Monitoring**:

  ```bash
  # Monitor for security events
  docker logs xg2g-app | grep -E "(429|5..|error|security)"
  ```

### Monitoring Setup

1. **Access Grafana**: <http://localhost:3000>
   - Username: `admin`
   - Password: from `GRAFANA_PASSWORD`

2. **Import Security Dashboard**:
   - Go to Dashboards → Import
   - Upload `docker/grafana/dashboards/security-dashboard.json`

3. **Configure Alerts**:
   - Prometheus rules auto-loaded from `security-alerts.yml`
   - Configure notification channels in Grafana

### Health Checks

```bash
# Application health
curl http://localhost:8080/healthz
curl http://localhost:8080/readyz

# Security validation
curl -H "Origin: https://evil.com" http://localhost:8080/api/status
# Should NOT include Access-Control-Allow-Origin: https://evil.com

# Rate limiting test
for i in {1..15}; do curl -s -o /dev/null -w "%{http_code}\n" http://localhost:8080/api/status; done
# Should show 429 after ~10 requests

# Metrics endpoint
curl http://localhost:9090/metrics | grep -E "(rate_limit|security|error)"
```

### Security Monitoring

Key metrics to watch:

- `http_requests_total{status_code="429"}` - Rate limit violations
- `http_requests_total{status_code=~"5.."}` - Server errors  
- `refresh_duration_seconds` - Performance degradation
- Log pattern: `"security"`, `"traversal"`, `"cors"`

### Backup & Recovery

```bash
# Backup configuration and data
docker run --rm -v xg2g_xg2g_data:/source -v $(pwd)/backup:/backup alpine \
  tar czf /backup/xg2g-data-$(date +%Y%m%d).tar.gz -C /source .

# Backup Grafana dashboards
docker exec xg2g-grafana grafana-cli admin export-dashboard > dashboards-backup.json
```

### Incident Response

If security alerts trigger:

1. **High Rate Limit Violations**:

   ```bash
   # Check source IPs
   docker logs xg2g-app | grep "429" | tail -20
   # Consider blocking IPs via firewall
   ```

2. **High Error Rate**:

   ```bash
   # Check application logs
   docker logs xg2g-app | grep -E "(error|5..)" | tail -50
   # Restart if needed: docker restart xg2g-app
   ```

3. **Security Events**:

   ```bash
   # Full security audit
   docker logs xg2g-app | grep -E "(security|traversal|cors)"
   # Review access patterns and consider IP blocking
   ```

### Performance Optimization

- **Resource Limits**: Set in docker-compose.production.yml
- **Log Rotation**: Configure with Docker logging driver
- **Caching**: Enable reverse proxy caching for static files
- **CDN**: Consider CDN for `/files/*` endpoints

### Rollback Plan

```bash
# Quick rollback to previous version
docker-compose -f docker-compose.production.yml down
docker pull your-registry/xg2g:previous-tag
# Update image tag in docker-compose.production.yml
docker-compose -f docker-compose.production.yml up -d
```

---

## 🛡️ Security Status: PRODUCTION READY

This setup implements:

- ✅ Defense-in-depth security layers
- ✅ Comprehensive monitoring & alerting  
- ✅ Rate limiting & DoS protection
- ✅ Input validation & path traversal prevention
- ✅ Information disclosure protection
- ✅ Container security hardening
- ✅ Automated security scanning in CI

**Recommendation**: This configuration is suitable for production deployment with proper network security and TLS termination.
