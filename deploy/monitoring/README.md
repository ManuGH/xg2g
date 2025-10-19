# xg2g Monitoring Stack

Complete monitoring setup with Grafana dashboards, Prometheus metrics, and alerting.

## Quick Start

1. **Copy environment file:**
   ```bash
   cp .env.example .env
   ```

2. **Edit `.env`:**
   - Set `GRAFANA_PASSWORD`
   - Set `XG2G_OWI_BASE` to your Enigma2 receiver IP
   - Set `XG2G_BOUQUET` to your bouquet name

3. **Start the stack:**
   ```bash
   docker-compose up -d
   ```

4. **Access dashboards:**
   - **Grafana**: http://localhost:3000 (admin / your-password)
   - **Prometheus**: http://localhost:9091
   - **xg2g API**: http://localhost:8080

## What's Included

- **xg2g** - Main application (v1.4.0 with HDHomeRun emulation)
- **Grafana** - Visualization dashboards
- **Prometheus** - Metrics collection & storage
- **AlertManager** - Alert routing & notifications
- **Node Exporter** - System metrics

## Dashboards

Two pre-configured Grafana dashboards:

1. **xg2g Overview** - High-level metrics
   - Request rates
   - Error rates
   - EPG refresh status

2. **xg2g Detailed** - Deep dive
   - Per-endpoint metrics
   - Response times
   - Resource usage

## Ports

| Service | Port | Description |
|---------|------|-------------|
| xg2g | 8080 | HTTP API |
| xg2g | 9090 | Prometheus metrics |
| xg2g | 1900/udp | SSDP discovery (HDHomeRun) |
| Grafana | 3000 | Dashboard UI |
| Prometheus | 9091 | Prometheus UI |
| AlertManager | 9093 | Alert management |

## Configuration

### HDHomeRun Emulation (v1.4.0)

Enable auto-discovery in Plex/Jellyfin:

```bash
XG2G_HDHR_ENABLED=true
XG2G_HDHR_FRIENDLY_NAME=xg2g Monitoring
```

### Audio Transcoding

Fix audio delay in browsers:

```bash
XG2G_ENABLE_AUDIO_TRANSCODING=true
```

### EPG Settings

```bash
XG2G_EPG_ENABLED=true
XG2G_EPG_DAYS=7
```

## Alerts

Default alerts configured:

- High error rate (>5% for 5 minutes)
- xg2g down
- EPG refresh failures

Configure AlertManager in `monitoring/alertmanager.yml`.

## Troubleshooting

### Grafana Login Failed

Check password in `.env`:
```bash
grep GRAFANA_PASSWORD .env
```

### No Metrics Visible

1. Check Prometheus scraping:
   - http://localhost:9091/targets

2. Check xg2g metrics:
   - http://localhost:9090/metrics

### HDHomeRun Not Discovered

1. Check UDP port 1900 is exposed:
   ```bash
   docker-compose ps
   ```

2. Check SSDP logs:
   ```bash
   docker logs xg2g-app | grep -i ssdp
   ```

## Updating

Pull latest images:
```bash
docker-compose pull
docker-compose up -d
```

## Cleanup

Stop and remove everything:
```bash
docker-compose down -v
```

Keep data, remove containers only:
```bash
docker-compose down
```
