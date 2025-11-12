# Security Hardening Guide

This document provides best practices and recommendations for securing xg2g deployments in production environments.

## Table of Contents

- [Authentication & Authorization](#authentication--authorization)
- [Network Security](#network-security)
- [Rate Limiting](#rate-limiting)
- [TLS/HTTPS Configuration](#tlshttps-configuration)
- [Data Security](#data-security)
- [Monitoring & Logging](#monitoring--logging)
- [Container Security](#container-security)
- [Deployment Recommendations](#deployment-recommendations)

---

## Authentication & Authorization

### API Token Protection

xg2g uses API tokens to protect sensitive endpoints like `/api/refresh`.

**Configuration:**
```bash
export XG2G_API_TOKEN="your-secure-random-token-here"
```

**Best Practices:**
- ✅ Generate tokens using cryptographically secure random generators
- ✅ Minimum 32 characters, alphanumeric + special characters
- ✅ Rotate tokens regularly (every 90 days)
- ✅ Store tokens in secrets management systems (not in git)
- ❌ Never use default or weak tokens like "admin", "password", "test"

**Token Generation:**
```bash
# Linux/macOS
openssl rand -base64 32

# Or using Python
python3 -c "import secrets; print(secrets.token_urlsafe(32))"
```

### OpenWebIF Authentication

If your OpenWebIF server requires authentication:

```bash
export XG2G_OWI_USERNAME="your-username"
export XG2G_OWI_PASSWORD="your-password"
```

**Security Notes:**
- Credentials are transmitted via HTTP Basic Auth
- **Always use HTTPS** for OpenWebIF if credentials are required
- Consider using read-only accounts for xg2g
- Avoid exposing OpenWebIF directly to the internet

### HDHomeRun Emulation

HDHomeRun endpoints are unauthenticated by design (for client compatibility):

```bash
export XG2G_HDHR_ENABLED=true
```

**Mitigation:**
- Place xg2g behind a reverse proxy with authentication
- Use firewall rules to restrict access to trusted networks
- Monitor access logs for suspicious activity

---

## Network Security

### Bind Address Configuration

**Default:** xg2g binds to `:8080` (all interfaces)

**Production Recommendation:**
```bash
export XG2G_LISTEN="127.0.0.1:8080"  # Localhost only
# OR
export XG2G_LISTEN="10.0.0.5:8080"   # Specific internal IP
```

**Why:**
- Prevents direct internet exposure
- Forces traffic through reverse proxy
- Reduces attack surface

### Firewall Rules

**Recommended iptables rules:**
```bash
# Allow from trusted networks only
iptables -A INPUT -p tcp -s 10.0.0.0/8 --dport 8080 -j ACCEPT
iptables -A INPUT -p tcp --dport 8080 -j DROP

# Allow SSDP for HDHomeRun discovery (if enabled)
iptables -A INPUT -p udp --dport 1900 -j ACCEPT  # From trusted network only
```

### Reverse Proxy Setup

**Recommended:** Place xg2g behind nginx/Caddy/Traefik

**Example nginx configuration:**
```nginx
server {
    listen 443 ssl http2;
    server_name xg2g.example.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    # Security headers
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    add_header X-Frame-Options "DENY" always;
    add_header X-Content-Type-Options "nosniff" always;
    add_header X-XSS-Protection "1; mode=block" always;

    # Rate limiting
    limit_req_zone $binary_remote_addr zone=xg2g_refresh:10m rate=5r/m;
    limit_req zone=xg2g_refresh burst=2 nodelay;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # Timeout configuration
        proxy_read_timeout 120s;
        proxy_connect_timeout 10s;
    }
}
```

---

## Rate Limiting

### Built-in Protections

xg2g has built-in server hardening (configurable via environment):

```bash
# Connection limits
export XG2G_SERVER_READ_TIMEOUT=5s
export XG2G_SERVER_WRITE_TIMEOUT=10s
export XG2G_SERVER_IDLE_TIMEOUT=120s
export XG2G_SERVER_MAX_HEADER_BYTES=1048576  # 1 MB
```

### Recommended Rate Limits

**Endpoint-specific limits:**

| Endpoint | Recommended Limit | Reason |
|----------|-------------------|--------|
| `/api/refresh` | 5 requests/minute | Expensive operation (fetches all channels) |
| `/api/status` | 60 requests/minute | Read-only, can be polled |
| `/healthz`, `/readyz` | 120 requests/minute | Health checks from orchestrators |
| `/playlist.m3u` | 30 requests/minute | Client playlist updates |
| `/xmltv.xml` | 10 requests/minute | Large file, infrequent updates |

### Implementation with nginx

```nginx
# Define rate limit zones
limit_req_zone $binary_remote_addr zone=refresh:10m rate=5r/m;
limit_req_zone $binary_remote_addr zone=status:10m rate=60r/m;
limit_req_zone $binary_remote_addr zone=playlist:10m rate=30r/m;

server {
    location /api/refresh {
        limit_req zone=refresh burst=2 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location /api/status {
        limit_req zone=status burst=10 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }

    location ~ \.(m3u|xml)$ {
        limit_req zone=playlist burst=5 nodelay;
        proxy_pass http://127.0.0.1:8080;
    }
}
```

### Circuit Breaker

xg2g has a built-in circuit breaker for OpenWebIF requests:

**Default:** 3 failures → 30 second open period

**Monitoring:**
```bash
# Check circuit breaker metrics
curl http://localhost:9090/metrics | grep circuit_breaker
```

---

## TLS/HTTPS Configuration

### Certificate Management

**Recommended:** Use Let's Encrypt with automatic renewal

**With Caddy (automatic HTTPS):**
```caddyfile
xg2g.example.com {
    reverse_proxy localhost:8080

    # Rate limiting
    rate_limit {
        zone refresh {
            match {
                path /api/refresh
            }
            rate 5/m
        }
    }
}
```

**With Certbot + nginx:**
```bash
certbot --nginx -d xg2g.example.com
```

### TLS Configuration

**Minimum TLS version:** TLS 1.2 (prefer TLS 1.3)

**Recommended cipher suites:**
```nginx
ssl_protocols TLSv1.2 TLSv1.3;
ssl_ciphers 'ECDHE-ECDSA-AES128-GCM-SHA256:ECDHE-RSA-AES128-GCM-SHA256:ECDHE-ECDSA-AES256-GCM-SHA384:ECDHE-RSA-AES256-GCM-SHA384';
ssl_prefer_server_ciphers off;
```

---

## Data Security

### Data Directory Protection

**Configuration:**
```bash
export XG2G_DATA_DIR="/var/lib/xg2g/data"
```

**Security measures:**
```bash
# Set secure permissions
mkdir -p /var/lib/xg2g/data
chown xg2g:xg2g /var/lib/xg2g/data
chmod 750 /var/lib/xg2g/data

# Prevent symlink attacks
# xg2g validates paths and follows symlinks securely
```

**Built-in protections:**
- Path traversal prevention (blocks `../` attempts)
- Symlink escape detection
- System directory blacklist (`/etc`, `/bin`, `/usr`, etc.)
- Absolute path enforcement

### Sensitive Data Logging

xg2g automatically redacts sensitive fields:

```go
// ✅ Logged as "using XG2G_API_TOKEN from environment (set)"
export XG2G_API_TOKEN="secret"

// ✅ URL credentials are masked in logs
export XG2G_OWI_BASE="http://user:pass@example.com"
// Logged as: "http://example.com" (user:pass removed)
```

**Log Review:**
```bash
# Check for accidentally logged secrets
grep -i "password\|token\|secret" /var/log/xg2g/app.log
```

### Secrets Management

**Production Recommendations:**

1. **Kubernetes Secrets:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: xg2g-secrets
type: Opaque
stringData:
  api-token: "your-secure-token"
  owi-username: "readonly-user"
  owi-password: "secure-password"
---
apiVersion: v1
kind: Pod
metadata:
  name: xg2g
spec:
  containers:
  - name: xg2g
    image: ghcr.io/manugh/xg2g:latest
    envFrom:
    - secretRef:
        name: xg2g-secrets
```

2. **Docker Secrets:**
```bash
echo "your-secure-token" | docker secret create xg2g_api_token -
docker service create --secret xg2g_api_token ghcr.io/manugh/xg2g:latest
```

3. **HashiCorp Vault:**
```bash
vault kv put secret/xg2g \
  api_token="your-secure-token" \
  owi_username="readonly" \
  owi_password="secure-pass"
```

---

## Monitoring & Logging

### Prometheus Metrics

**Enable metrics endpoint:**
```bash
export XG2G_METRICS_LISTEN=":9090"
```

**Security considerations:**
- Bind to localhost only: `XG2G_METRICS_LISTEN="127.0.0.1:9090"`
- Use firewall rules to restrict access
- Place behind authenticated reverse proxy

**Key metrics to monitor:**
```promql
# Circuit breaker state
xg2g_circuit_breaker_state

# Request rate
rate(xg2g_http_requests_total[5m])

# Error rate
rate(xg2g_http_requests_total{status=~"5.."}[5m])

# Refresh failures
rate(xg2g_refresh_failures_total[5m])
```

### Alerting Rules

**Recommended Prometheus alerts:**
```yaml
groups:
- name: xg2g
  rules:
  - alert: HighErrorRate
    expr: rate(xg2g_http_requests_total{status=~"5.."}[5m]) > 0.1
    for: 5m
    labels:
      severity: warning
    annotations:
      summary: "High error rate detected"

  - alert: CircuitBreakerOpen
    expr: xg2g_circuit_breaker_state == 1
    for: 2m
    labels:
      severity: warning
    annotations:
      summary: "Circuit breaker is open"

  - alert: RefreshFailures
    expr: rate(xg2g_refresh_failures_total[10m]) > 0.5
    for: 10m
    labels:
      severity: critical
    annotations:
      summary: "Persistent refresh failures"
```

### Structured Logging

xg2g uses structured JSON logging:

```json
{
  "level": "info",
  "service": "xg2g",
  "version": "1.4.0",
  "component": "api",
  "event": "request.complete",
  "method": "POST",
  "path": "/api/refresh",
  "status": 200,
  "duration_ms": 1234,
  "remote_addr": "10.0.0.5",
  "time": "2025-01-15T10:30:00Z"
}
```

**Log aggregation with Loki/Elasticsearch:**
```bash
# Docker Compose example
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    logging:
      driver: "json-file"
      options:
        max-size: "10m"
        max-file: "3"
        labels: "service=xg2g"
```

---

## Container Security

### Non-root User

xg2g container runs as non-root by default:

```dockerfile
USER nonroot:nonroot
```

**Verification:**
```bash
docker run --rm ghcr.io/manugh/xg2g:latest id
# uid=65532(nonroot) gid=65532(nonroot)
```

### Image Scanning

**Recommended:** Scan images before deployment

```bash
# Using Trivy
trivy image ghcr.io/manugh/xg2g:latest

# Using Grype
grype ghcr.io/manugh/xg2g:latest
```

### Container Runtime Security

**Docker security options:**
```bash
docker run -d \
  --name xg2g \
  --read-only \
  --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  --tmpfs /tmp:rw,noexec,nosuid,size=100m \
  -v /var/lib/xg2g/data:/data:rw \
  ghcr.io/manugh/xg2g:latest
```

**Kubernetes Pod Security:**
```yaml
apiVersion: v1
kind: Pod
metadata:
  name: xg2g
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 65532
    fsGroup: 65532
    seccompProfile:
      type: RuntimeDefault
  containers:
  - name: xg2g
    image: ghcr.io/manugh/xg2g:latest
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
    volumeMounts:
    - name: data
      mountPath: /data
    - name: tmp
      mountPath: /tmp
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: xg2g-data
  - name: tmp
    emptyDir: {}
```

---

## Deployment Recommendations

### Production Checklist

- [ ] API token configured (32+ characters, random)
- [ ] Bind address restricted (localhost or internal IP)
- [ ] Reverse proxy with TLS configured
- [ ] Rate limiting enabled
- [ ] Firewall rules configured
- [ ] Data directory permissions set (750)
- [ ] Secrets stored in vault/secrets manager
- [ ] Metrics endpoint secured
- [ ] Alerting rules configured
- [ ] Log aggregation setup
- [ ] Container security hardened
- [ ] Regular security updates scheduled

### Network Architecture

**Recommended deployment:**

```
Internet
   │
   ↓
┌─────────────────┐
│ Reverse Proxy   │ ← TLS termination, rate limiting, auth
│ (nginx/Caddy)   │
└─────────────────┘
   │
   ↓
┌─────────────────┐
│ xg2g            │ ← Binds to localhost:8080
│ (localhost)     │
└─────────────────┘
   │
   ↓
┌─────────────────┐
│ OpenWebIF       │ ← Internal network only
│ (Satellite Box) │
└─────────────────┘
```

### Update Strategy

**Recommended:** Enable automatic security updates

```bash
# Kubernetes with automatic image updates
kubectl set image deployment/xg2g \
  xg2g=ghcr.io/manugh/xg2g:latest

# Docker with Watchtower
docker run -d \
  --name watchtower \
  -v /var/run/docker.sock:/var/run/docker.sock \
  containrrr/watchtower \
  --interval 86400 \  # Check daily
  xg2g
```

---

## Security Contacts

**Report security vulnerabilities:**
- GitHub Security Advisories: https://github.com/ManuGH/xg2g/security/advisories
- Email: [security contact if available]

**Security updates:**
- Subscribe to GitHub releases: https://github.com/ManuGH/xg2g/releases
- Watch security advisories

---

## Additional Resources

- [OWASP Top 10](https://owasp.org/www-project-top-ten/)
- [CIS Docker Benchmark](https://www.cisecurity.org/benchmark/docker)
- [Kubernetes Security Best Practices](https://kubernetes.io/docs/concepts/security/)
- [Let's Encrypt](https://letsencrypt.org/)
