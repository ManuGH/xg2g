# xg2g Grafana Dashboards

This directory contains pre-configured Grafana dashboard templates for monitoring xg2g.

## Dashboards

### 1. xg2g Overview (`xg2g-overview.json`)
High-level system overview showing the health and status of xg2g.

**Panels:**
- **Total Channels** - Current number of channels in playlist (gauge)
- **Total Bouquets** - Number of bouquets being processed (gauge)
- **Last Refresh** - Time since last successful refresh (stat)
- **Playlist Files Valid** - Whether M3U and XMLTV files are readable (timeseries)
- **Refresh Duration** - p95/p99 percentiles of refresh operation time (timeseries)
- **Refresh Errors by Stage** - Breakdown of failures by stage (bouquets, services, etc.)
- **Channel Types Distribution** - HD/SD/Radio/Unknown channel breakdown (timeseries)
- **Services per Bouquet** - Number of channels in each bouquet (timeseries)

**Use Cases:**
- At-a-glance system health monitoring
- Verifying refresh operations are successful
- Tracking channel counts over time

---

### 2. xg2g Performance (`xg2g-performance.json`)
Detailed API performance metrics and request handling statistics.

**Panels:**
- **Request Latency by Endpoint** - p50/p95/p99 latencies per endpoint (timeseries)
- **Requests per Second by Endpoint** - Traffic breakdown by endpoint (timeseries)
- **Success Rate (2xx)** - Percentage of successful requests (gauge)
- **Requests In Flight** - Current concurrent requests (stat)
- **HTTP Status Codes** - Distribution of response codes over time (timeseries)
- **Response Size (p95)** - Response payload sizes per endpoint (timeseries)
- **File Cache Hit Rate** - Cache hits (304) vs misses (200) for static files
- **Security: File Requests Denied** - Security violations by reason (path traversal, etc.)
- **Panics Recovered** - HTTP handler panic recovery events

**Use Cases:**
- Identifying slow endpoints
- Detecting performance regressions
- Monitoring API error rates
- Security incident detection

---

### 3. xg2g Streaming & EPG (`xg2g-streaming.json`)
Streaming-specific metrics including EPG collection, stream URL generation, and channel processing.

**Panels:**
- **Stream URL Build Success Rate** - Percentage of successful stream URL generations (gauge)
- **EPG Programmes Collected** - Total EPG events in last refresh (stat)
- **Channels with EPG Data** - How many channels have EPG data (stat)
- **XMLTV Status** - Whether XMLTV generation is enabled (stat)
- **Stream URL Build Attempts** - Success/failure breakdown over time (timeseries)
- **EPG Collection Duration** - p95/p99 time to collect all EPG data (timeseries)
- **EPG Request Status** - Success/error/timeout breakdown per channel (timeseries)
- **XMLTV Channels Written** - Number of channels in XMLTV file (timeseries)
- **Error Counters** - XMLTV write errors, bouquet discovery errors, config errors
- **Service Resolution by Bouquet** - Success/failure per bouquet (timeseries)

**Use Cases:**
- EPG data quality monitoring
- Stream URL generation troubleshooting
- Identifying problematic bouquets
- XMLTV generation health

---

## Installation

### Option 1: Import via Grafana UI
1. Open Grafana
2. Navigate to **Dashboards** → **Import**
3. Upload each JSON file
4. Select your Prometheus datasource
5. Click **Import**

### Option 2: Provision via Configuration
Add to your Grafana `provisioning/dashboards/xg2g.yaml`:

```yaml
apiVersion: 1

providers:
  - name: 'xg2g'
    orgId: 1
    folder: 'xg2g'
    type: file
    disableDeletion: false
    updateIntervalSeconds: 10
    allowUiUpdates: true
    options:
      path: /path/to/xg2g/deploy/monitoring/grafana/dashboards
```

### Option 3: Docker Compose
If using the provided `docker-compose.yml`:

```yaml
volumes:
  - ./deploy/monitoring/grafana/dashboards:/etc/grafana/provisioning/dashboards/xg2g:ro
```

---

## Metrics Reference

### Business Metrics
- `xg2g_channels` - Total channels in last refresh
- `xg2g_bouquets_total` - Number of bouquets discovered
- `xg2g_services_discovered{bouquet}` - Services per bouquet
- `xg2g_channel_types{type}` - Channels by type (hd/sd/radio/unknown)
- `xg2g_last_refresh_timestamp` - Unix timestamp of last refresh
- `xg2g_refresh_duration_seconds` - Histogram of refresh durations

### HTTP Performance
- `xg2g_http_request_duration_seconds{method,path,status}` - Request latency histogram
- `xg2g_http_requests_in_flight` - Current concurrent requests
- `xg2g_http_request_size_bytes{method,path}` - Request payload sizes
- `xg2g_http_response_size_bytes{method,path,status}` - Response payload sizes

### Streaming & EPG
- `xg2g_stream_url_build_total{outcome}` - Stream URL generation (success/failure)
- `xg2g_epg_programmes_collected` - Total EPG programmes in last refresh
- `xg2g_epg_channels_with_data` - Channels with EPG data
- `xg2g_epg_collection_duration_seconds` - EPG collection time histogram
- `xg2g_epg_requests_total{status}` - EPG requests (success/error/timeout)
- `xg2g_xmltv_enabled` - XMLTV generation status (1=enabled, 0=disabled)
- `xg2g_xmltv_channels_written` - Channels in XMLTV file

### Error Tracking
- `xg2g_refresh_failures_total{stage}` - Refresh failures by stage
- `xg2g_http_panics_total{path}` - Recovered panics per endpoint
- `xg2g_file_requests_denied_total{reason}` - Security violations
- `xg2g_config_validation_errors_total` - Config validation failures

### File Operations
- `xg2g_playlist_file_valid{type}` - File validity (m3u/xmltv)
- `xg2g_file_cache_hits_total` - HTTP 304 responses
- `xg2g_file_cache_misses_total` - HTTP 200 responses

---

## Alerting Recommendations

Consider creating alerts for:

1. **Refresh Failures**: `rate(xg2g_refresh_failures_total[5m]) > 0`
2. **High Error Rate**: `rate(xg2g_http_request_duration_seconds_count{status=~"5.."}[5m]) > 0.05`
3. **Stream URL Build Failures**: `rate(xg2g_stream_url_build_total{outcome="failure"}[5m]) > 0`
4. **EPG Collection Issues**: `xg2g_epg_channels_with_data < 50` (adjust threshold)
5. **File Validity**: `xg2g_playlist_file_valid == 0`
6. **Panics**: `rate(xg2g_http_panics_total[5m]) > 0`

---

## Customization

All dashboards use a templated Prometheus datasource (`${DS_PROMETHEUS}`). You can:
- Adjust refresh intervals (default: 10s)
- Modify time ranges (default: last 1 hour)
- Add/remove panels as needed
- Create custom variables for filtering by bouquet

---

## Support

For issues or questions:
- GitHub: https://github.com/ManuGH/xg2g
- Documentation: See project README

---

**Generated:** 2025 Sprint 2 Implementation
**Version:** 1.0.0
**Grafana Version:** 10.0.0+
