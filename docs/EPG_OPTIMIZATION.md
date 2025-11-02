# EPG Fetch Optimization

## Overview

xg2g supports two EPG fetching strategies to optimize performance based on your setup:

1. **Per-Service** (default) - Fetches EPG for each channel individually
2. **Bouquet** (fast) - Fetches EPG for entire bouquet in one request

## Comparison

| Strategy | Requests | Speed | Receiver Load | Best For |
|----------|----------|-------|---------------|----------|
| **per-service** | N (one per channel) | Moderate | Higher | Channels from multiple bouquets |
| **bouquet** | 1 (single request) | **Fast** | Lower | All channels from one bouquet |

### Per-Service Strategy (Default)

```
Channel 1 → EPG Request 1 ┐
Channel 2 → EPG Request 2 ├→ Parallel execution
Channel 3 → EPG Request 3 │  (max 5 concurrent)
...                       │
Channel N → EPG Request N ┘
```

**Pros:**
- Works with channels from different bouquets
- More granular error handling per channel
- Better for mixed setups

**Cons:**
- N API calls to receiver
- Higher network overhead
- Can be slow for many channels (150+ channels)

### Bouquet Strategy (Optimized)

```
Bouquet "Premium" → Single EPG Request → All channel EPG data
```

**Pros:**
- ✅ **Single API call** - 50-100x faster
- ✅ **Lower receiver load** - Reduced CPU/network usage
- ✅ **Faster refresh** - Ideal for 100+ channels
- ✅ **Exact SRef matching** - Reliable channel identification

**Cons:**
- Only works when all channels are from the same bouquet
- Fallback to per-service if bouquet not found

## Configuration

### Environment Variable

```bash
XG2G_EPG_SOURCE=bouquet
```

### YAML Configuration

```yaml
epg:
  enabled: true
  days: 7
  source: bouquet  # "bouquet" or "per-service" (default)
```

### Docker Compose

```yaml
services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    environment:
      - XG2G_EPG_SOURCE=bouquet
      - XG2G_BOUQUET=Premium
      - XG2G_EPG_DAYS=7
```

## Performance Comparison

### Example: 150 Channels, 7 Days EPG

| Strategy | Requests | Duration | Receiver CPU | Network |
|----------|----------|----------|--------------|---------|
| **per-service** | 150 | ~45-60s | Medium | ~15MB |
| **bouquet** | 1 | ~3-5s | Low | ~2MB |

**Result:** Bouquet mode is **10-15x faster** with 87% less network traffic.

## When to Use Each Strategy

### Use Per-Service When:
- ❌ Channels come from multiple bouquets
- ❌ Selective EPG fetching for specific channels
- ❌ Testing/debugging individual channel EPG

### Use Bouquet When:
- ✅ All channels from single bouquet (common case)
- ✅ You have 50+ channels
- ✅ You want faster refresh times
- ✅ You want to reduce receiver load

## Matching Strategy

### Bouquet Mode Matching

EPG events are matched to channels using **exact SRef matching**:

1. Extract service reference (SRef) from each channel's stream URL
2. Build SRef → tvg-id lookup map
3. Match EPG events by SRef field
4. Group events by channel

**Example:**
```
Channel Stream URL: http://receiver/1:0:19:132F:3EF:1:C00000:0:0:0:
Extracted SRef:     1:0:19:132F:3EF:1:C00000:0:0:0:
EPG Event SRef:     1:0:19:132F:3EF:1:C00000:0:0:0:
Match: ✅
```

This ensures **100% accurate matching** without fuzzy logic.

## Migration Guide

### Switching from Per-Service to Bouquet

**1. Verify Single Bouquet:**
```bash
# Check that all channels are from one bouquet
curl http://receiver/api/getservices?sRef=YOUR_BOUQUET_REF
```

**2. Update Configuration:**
```bash
# Add to docker-compose.yml or .env
XG2G_EPG_SOURCE=bouquet
```

**3. Test:**
```bash
# Trigger refresh and check logs
curl -X POST http://localhost:8080/api/v1/refresh
docker logs xg2g | grep "EPG collected"
```

Expected output:
```
INFO EPG collected via bouquet endpoint
     total_programmes=2450 channels_with_data=150 total_channels=150
```

**4. Monitor:**
- EPG programmes count should be similar to per-service
- Refresh duration should be significantly lower
- Check for any missing EPG data

### Rollback to Per-Service

If you experience issues:

```bash
# Remove or comment out
# XG2G_EPG_SOURCE=bouquet

# Or explicitly set
XG2G_EPG_SOURCE=per-service
```

Restart container:
```bash
docker restart xg2g
```

## Troubleshooting

### No EPG Data After Switching to Bouquet

**Check:**
1. Bouquet name is correct: `XG2G_BOUQUET=YourBouquetName`
2. Logs show: "Fetching EPG for bouquet" (not "falling back to per-service")
3. Receiver supports bouquet EPG endpoint

**Debug:**
```bash
# Check available bouquets
curl http://receiver/web/bouquets

# Test bouquet EPG manually
curl "http://receiver/web/epgbouquet?bRef=BOUQUET_REF&time=-1"
```

### Some Channels Missing EPG

**Cause:** SRef mismatch between stream URL and EPG event

**Solution:**
1. Check logs for "No channel match for EPG event"
2. Verify stream URLs contain correct SRefs
3. Try per-service mode for affected channels only (future feature)

### Bouquet Mode Slower Than Expected

**Check:**
1. Network latency to receiver
2. Receiver CPU load (other processes)
3. Number of days configured (XG2G_EPG_DAYS)

**Optimize:**
```bash
# Reduce EPG days if not needed
XG2G_EPG_DAYS=3

# Increase timeout for large bouquets
XG2G_EPG_TIMEOUT_MS=30000
```

## Best Practices

1. **Use Bouquet Mode** if all channels from one bouquet
2. **Monitor First Refresh** after switching modes
3. **Compare EPG Counts** before/after switch
4. **Set Reasonable Timeout** based on channel count
5. **Enable Debug Logging** during migration:
   ```bash
   XG2G_LOG_LEVEL=debug
   ```

## Future Enhancements

Potential improvements for future versions:

- [ ] Hybrid mode: bouquet + per-service fallback
- [ ] Multi-bouquet support (fetch multiple bouquets)
- [ ] EPG caching layer to reduce refresh frequency
- [ ] Selective channel EPG (exclude unwanted channels)

## Related Configuration

```yaml
epg:
  enabled: true
  days: 7                  # EPG days to fetch (1-14)
  source: bouquet          # Fetch strategy
  maxConcurrency: 5        # Only for per-service mode
  timeoutMs: 15000         # Request timeout
  retries: 2               # Retry attempts
  fuzzyMax: 2              # Fuzzy match distance (unused in bouquet mode)
```

## Metrics

Monitor EPG performance:

```promql
# EPG collection duration
xg2g_epg_update_duration_seconds

# Channels with EPG data
xg2g_epg_channels_success

# Total programmes collected
xg2g_epg_programmes_total
```

## Support

If you experience issues with bouquet mode:
1. Check logs: `docker logs xg2g`
2. Verify bouquet exists: Check receiver web interface
3. Open issue: https://github.com/ManuGH/xg2g/issues

Include:
- EPG source mode used
- Number of channels
- Bouquet name
- Receiver model/firmware
- Relevant log excerpts
