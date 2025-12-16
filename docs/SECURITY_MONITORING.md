# Security Monitoring Guide

## Overview

xg2g includes comprehensive security monitoring to detect and log unauthorized access attempts. This document describes the security features and how to monitor them.

## Security Features

### 1. Frontend Security Interceptor

**Location**: `webui/src/client/interceptor.js`

**What it does**:
- Automatically intercepts ALL HTTP responses
- Detects 401 Unauthorized errors globally
- Triggers authentication modal when credentials are missing/invalid
- Logs security events to browser console

**Benefits**:
- No need to manually check for 401 in every component
- Prevents confusion from "Not Found" errors when auth fails
- Automatic session recovery

**Console Logs**:
```javascript
[Security] API token loaded from localStorage
[Security] 401 Unauthorized detected on: /api/v2/system/health
[Security] API token updated
[Security] API token removed
```

### 2. Backend Security Logging

**Location**: `internal/api/http.go` (authMiddleware)

**Security Events Logged**:

#### Missing Authorization Header
```json
{
  "level": "warn",
  "event": "auth.missing_header",
  "remote_addr": "192.168.1.100:54321",
  "path": "/api/v2/system/health",
  "user_agent": "Mozilla/5.0...",
  "message": "unauthorized access attempt - missing authorization header"
}
```

#### Malformed Authorization Header
```json
{
  "level": "warn",
  "event": "auth.malformed_header",
  "remote_addr": "192.168.1.100:54321",
  "path": "/api/v2/channels",
  "message": "unauthorized access attempt - malformed authorization header"
}
```

#### Invalid Bearer Token (Potential Brute Force)
```json
{
  "level": "warn",
  "event": "auth.invalid_token",
  "remote_addr": "192.168.1.100:54321",
  "path": "/api/v2/system/health",
  "user_agent": "curl/8.0.1",
  "message": "SECURITY ALERT: invalid bearer token - potential unauthorized access attempt"
}
```

## Monitoring Security Logs

### Real-time Monitoring

Monitor authentication failures in real-time:

```bash
# Watch for all authentication events
tail -f /tmp/daemon.log | grep -E "auth\.(missing_header|malformed_header|invalid_token)"

# Watch for SECURITY ALERTS only
tail -f /tmp/daemon.log | grep "SECURITY ALERT"
```

### Prometheus Metrics

Security-related metrics are exposed on the metrics endpoint (if enabled):

```bash
# Enable metrics server
export XG2G_METRICS_LISTEN=":9090"

# Query metrics
curl http://localhost:9090/metrics | grep xg2g_http
```

Relevant metrics:
- `xg2g_http_requests_total{status="401"}` - Count of unauthorized requests
- `xg2g_http_requests_total{status="403"}` - Count of forbidden requests

### Grafana Alerts

Example Grafana alert rule for detecting brute force attempts:

```yaml
- alert: PotentialBruteForceAttack
  expr: rate(xg2g_http_requests_total{status="401"}[5m]) > 10
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Potential brute force attack detected"
    description: "More than 10 failed auth attempts per minute for 5 minutes"
```

## Security Best Practices

### 1. Strong API Tokens

Generate cryptographically secure tokens:

```bash
# Generate a random 32-byte token (256 bits)
openssl rand -base64 32
```

Set in `.env`:
```bash
XG2G_API_TOKEN=your-generated-token-here
```

### 2. Rotate Tokens Regularly

Establish a token rotation schedule:

```bash
# Rotate token monthly
openssl rand -base64 32 > /etc/xg2g/api_token.txt
export XG2G_API_TOKEN=$(cat /etc/xg2g/api_token.txt)
```

### 3. Monitor Failed Auth Attempts

Set up alerts for unusual patterns:

```bash
# Count failed auth attempts in last hour
grep "auth.invalid_token" /tmp/daemon.log | \
  grep "$(date +%Y-%m-%d)" | \
  wc -l
```

### 4. IP-based Rate Limiting

The built-in rate limiter protects against brute force:

```bash
# Default: 600 requests per 10 minutes per IP
# Customize in code: internal/api/middleware/ratelimit.go
```

### 5. Use TLS/HTTPS in Production

Always enable TLS for production deployments:

```bash
export XG2G_TLS_ENABLED=true
export XG2G_TLS_CERT=/path/to/cert.pem
export XG2G_TLS_KEY=/path/to/key.pem
```

## Incident Response

### If You See Suspicious Activity

1. **Check the logs** for the attacker's IP:
   ```bash
   grep "SECURITY ALERT" /tmp/daemon.log | grep -oP 'remote_addr":"[^"]+' | sort | uniq -c | sort -nr
   ```

2. **Block the IP** using firewall rules:
   ```bash
   iptables -A INPUT -s ATTACKER_IP -j DROP
   ```

3. **Rotate your API token immediately**:
   ```bash
   openssl rand -base64 32
   # Update XG2G_API_TOKEN in .env
   # Restart daemon
   ```

4. **Review access logs** for successful breaches:
   ```bash
   grep "status\":200" /tmp/daemon.log | grep "ATTACKER_IP"
   ```

## Automated Security Monitoring Script

Save this as `monitor_security.sh`:

```bash
#!/bin/bash
# xg2g Security Monitor
# Alerts on suspicious authentication patterns

LOG_FILE="/tmp/daemon.log"
ALERT_THRESHOLD=5  # Alert after 5 failed attempts
CHECK_INTERVAL=300  # Check every 5 minutes

while true; do
  failed_attempts=$(grep "auth.invalid_token" "$LOG_FILE" | \
    grep "$(date +%Y-%m-%d)" | \
    wc -l)

  if [ "$failed_attempts" -gt "$ALERT_THRESHOLD" ]; then
    echo "⚠️  SECURITY ALERT: $failed_attempts failed auth attempts today!"
    # Send notification (email, Slack, etc.)
  fi

  sleep $CHECK_INTERVAL
done
```

Run in background:
```bash
chmod +x monitor_security.sh
nohup ./monitor_security.sh &
```

## FAQ

### Q: Why do I see 401 errors in normal usage?

**A**: This is expected when:
- First visiting the WebUI (before entering token)
- Token expired or removed from localStorage
- Token changed on server but not in client

The frontend automatically shows the auth modal in these cases.

### Q: How do I disable auth for local development?

**A**: Set an empty token (not recommended):
```bash
# WARNING: This disables authentication entirely
export XG2G_API_TOKEN=""
```

### Q: Can I use different tokens for different users?

**A**: Currently, xg2g uses a single shared API token. For multi-user scenarios, consider:
- Using a reverse proxy (e.g., Traefik, Caddy) for user authentication
- Implementing OAuth2/OIDC upstream
- Each user having their own xg2g instance

## References

- [Rate Limiting Configuration](../internal/api/middleware/ratelimit.go)
- [Authentication Middleware](../internal/api/http.go)
- [Frontend Interceptor](../webui/src/client/interceptor.js)
- [Prometheus Metrics](../internal/api/metrics.go)
