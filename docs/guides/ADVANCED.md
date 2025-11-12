# Advanced Configuration

This document covers advanced tuning, monitoring, and production deployment options.

---

## Environment Variables (Complete List)

### Core Settings

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_DATA` | path | `./data` | Data directory for generated files |
| `XG2G_LISTEN` | address | `:8080` | HTTP listen address |
| `XG2G_OWI_BASE` | url | - | OpenWebIF base URL (required) |
| `XG2G_OWI_USER` | string | - | OpenWebIF username (optional) |
| `XG2G_OWI_PASS` | string | - | OpenWebIF password (optional) |
| `XG2G_BOUQUET` | string | - | Bouquet name(s) - comma-separated for multiple |
| `XG2G_STREAM_PORT` | int | `8001` | Enigma2 streaming port |
| `XG2G_PICON_BASE` | url | `${XG2G_OWI_BASE}` | Picon base URL |
| `XG2G_XMLTV` | string | `xmltv.xml` | XMLTV output filename |
| `XG2G_PLAYLIST_FILENAME` | string | `playlist.m3u` | M3U output filename |
| `XG2G_FUZZY_MAX` | int | `2` | Max Levenshtein distance for fuzzy matching |
| `XG2G_INITIAL_REFRESH` | bool | `false` | Trigger refresh on startup |

### EPG Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_EPG_ENABLED` | bool | `false` | Enable EPG data collection |
| `XG2G_EPG_DAYS` | int | `7` | Days of EPG to fetch (1-14) |
| `XG2G_EPG_MAX_CONCURRENCY` | int | `3` | Parallel EPG requests (1-10) |
| `XG2G_EPG_TIMEOUT_MS` | int | `15000` | EPG request timeout (ms) |
| `XG2G_EPG_RETRIES` | int | `2` | EPG retry attempts (0-5) |

### OpenWebIF Client Tuning

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_OWI_TIMEOUT_MS` | int | `10000` | Request timeout per attempt (ms) |
| `XG2G_OWI_RETRIES` | int | `3` | Max retry attempts (0-10) |
| `XG2G_OWI_BACKOFF_MS` | int | `500` | Base backoff delay (ms) |
| `XG2G_OWI_MAX_BACKOFF_MS` | int | `2000` | Maximum backoff delay (ms) |

### HTTP Server Configuration

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_SERVER_READ_TIMEOUT` | duration | `15s` | HTTP read timeout |
| `XG2G_SERVER_WRITE_TIMEOUT` | duration | `15s` | HTTP write timeout |
| `XG2G_SERVER_IDLE_TIMEOUT` | duration | `60s` | HTTP idle timeout |
| `XG2G_SERVER_MAX_HEADER_BYTES` | int | `1048576` | Max header size (bytes) |
| `XG2G_SERVER_SHUTDOWN_TIMEOUT` | duration | `10s` | Graceful shutdown timeout |

### HTTP Client Tuning

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_HTTP_MAX_IDLE_CONNS` | int | `100` | Max idle connections |
| `XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST` | int | `10` | Max idle per host |
| `XG2G_HTTP_MAX_CONNS_PER_HOST` | int | `0` | Max total per host (0=unlimited) |
| `XG2G_HTTP_IDLE_TIMEOUT` | duration | `90s` | Idle connection timeout |
| `XG2G_HTTP_ENABLE_HTTP2` | bool | `true` | Enable HTTP/2 |

### Monitoring

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_METRICS_LISTEN` | address | - | Prometheus metrics address (e.g. `:9090`) |

### Rate Limiting

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_RATELIMIT_ENABLED` | bool | `false` | Enable rate limiting |
| `XG2G_RATELIMIT_RPS` | int | `5` | Requests per second |
| `XG2G_RATELIMIT_BURST` | int | `10` | Burst capacity |
| `XG2G_RATELIMIT_WHITELIST` | string | - | Comma-separated IPs/CIDRs to bypass |

### API Security

| Variable | Type | Default | Description |
|----------|------|---------|-------------|
| `XG2G_API_TOKEN` | string | - | Bearer token for `/api/refresh` endpoint |

---

## Performance Tuning

### Slow or Overloaded Receiver

If your Enigma2 receiver is slow or overloaded:

```bash
XG2G_OWI_TIMEOUT_MS=20000
XG2G_OWI_BACKOFF_MS=1000
XG2G_EPG_MAX_CONCURRENCY=2
```

### Fast Local Network

For fast, reliable local networks:

```bash
XG2G_OWI_TIMEOUT_MS=5000
XG2G_OWI_BACKOFF_MS=200
XG2G_EPG_MAX_CONCURRENCY=8
```

### Unreliable Network

For unstable network connections:

```bash
XG2G_OWI_RETRIES=5
XG2G_OWI_MAX_BACKOFF_MS=5000
XG2G_EPG_RETRIES=3
```

---

## Monitoring with Prometheus

Enable Prometheus metrics:

```bash
XG2G_METRICS_LISTEN=:9090
```

### Available Metrics

**Performance:**
- `xg2g_openwebif_request_duration_seconds` - Request latencies (p50/p95/p99)
- `xg2g_openwebif_request_retries_total` - Retry attempts by operation
- `xg2g_openwebif_request_failures_total` - Failures by error class
- `xg2g_openwebif_request_success_total` - Successful requests

**Security:**
- `xg2g_file_requests_denied_total{reason}` - Blocked requests by attack type
- `xg2g_file_requests_allowed_total` - Successful file serves
- `xg2g_http_requests_total{status,endpoint}` - HTTP responses by status code

### Monitoring Stack

Deploy complete monitoring with Grafana and AlertManager:

```bash
docker-compose -f deploy/monitoring/docker-compose.yml up -d
```

Access:
- Grafana: http://localhost:3000 (admin/admin)
- Prometheus: http://localhost:9091
- AlertManager: http://localhost:9093

---

## Production Deployment

See [PRODUCTION.md](PRODUCTION.md) for detailed production deployment guides.

### Recommended Directory Structure

```bash
/opt/xg2g/
├── config/
│   ├── docker-compose.yml
│   └── .env
└── data/
    ├── playlist.m3u
    └── xmltv.xml
```

### systemd Service

Example systemd unit:

```ini
[Unit]
Description=xg2g IPTV Converter
After=network.target

[Service]
Type=simple
User=xg2g
Group=xg2g
WorkingDirectory=/opt/xg2g
ExecStart=/usr/local/bin/xg2g
Restart=always
RestartSec=10

Environment="XG2G_DATA=/opt/xg2g/data"
Environment="XG2G_OWI_BASE=http://receiver.local"
Environment="XG2G_BOUQUET=Favourites"
Environment="XG2G_EPG_ENABLED=true"

[Install]
WantedBy=multi-user.target
```

---

## Security

### API Token Protection

Protect the refresh endpoint with an API token:

```bash
XG2G_API_TOKEN=your-secret-token-here
```

Usage:

```bash
curl -H "X-API-Token: your-secret-token-here" \
  -X POST http://localhost:8080/api/refresh
```

Generate a secure token:

```bash
openssl rand -hex 16
```

### Rate Limiting

Enable rate limiting to prevent abuse:

```bash
XG2G_RATELIMIT_ENABLED=true
XG2G_RATELIMIT_RPS=10
XG2G_RATELIMIT_BURST=20
XG2G_RATELIMIT_WHITELIST=192.168.1.0/24,10.0.0.0/8
```

### File Serving Security

The `/files/*` endpoint includes protection against:
- Path traversal attacks (`../`)
- Absolute path access
- Null byte injection
- Invalid characters

All blocked attempts are logged with metrics.

---

## Advanced Scenarios

### Multiple Instances

Run multiple xg2g instances for different bouquets:

```yaml
services:
  xg2g-hd:
    image: ghcr.io/manugh/xg2g:latest
    environment:
      - XG2G_BOUQUET=HD_Channels
      - XG2G_LISTEN=:8081
    ports:
      - "8081:8081"

  xg2g-radio:
    image: ghcr.io/manugh/xg2g:latest
    environment:
      - XG2G_BOUQUET=Radio
      - XG2G_LISTEN=:8082
    ports:
      - "8082:8082"
```

### Automated Refresh

Use cron to trigger periodic refreshes:

```bash
# Every 6 hours
0 */6 * * * curl -X POST http://localhost:8080/api/refresh
```

Or use `XG2G_INITIAL_REFRESH=true` with container restarts.

---

## Troubleshooting

### Enable Debug Logging

```bash
XG2G_LOG_LEVEL=debug
```

### High Memory Usage

Reduce EPG concurrency and days:

```bash
XG2G_EPG_MAX_CONCURRENCY=2
XG2G_EPG_DAYS=3
```

### Connection Pooling Issues

Adjust HTTP client settings:

```bash
XG2G_HTTP_MAX_IDLE_CONNS_PER_HOST=5
XG2G_HTTP_IDLE_TIMEOUT=60s
```

---

## Further Reading

- [PRODUCTION.md](PRODUCTION.md) - Production deployment & operations
