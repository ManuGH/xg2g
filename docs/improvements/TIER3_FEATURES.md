# Tier 3: Nice-to-Have Features (Optional)

## Overview
Diese Features sind **optional** und bringen zusätzliche Funktionalität für spezielle Use Cases.

---

## 3.1 Safari/iOS AAC Validation Tests (4h)

### Problem
Keine automatischen Tests für Safari/iOS Kompatibilität (Mode 2: Audio Proxy)

### Lösung

**Datei:** `test/integration/safari_test.go` (neu)

```go
package integration

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSafariAACCompatibility(t *testing.T) {
	// Start xg2g in Mode 2 (Audio Proxy)
	server := setupMode2Server(t)
	defer server.Close()

	// Simulate Safari User-Agent
	req, _ := http.NewRequest("GET", server.URL+"/stream/test", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/15.0 Mobile/15E148 Safari/604.1")
	req.Header.Set("Accept", "audio/aac,audio/mpeg")

	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)

	// Verify ADTS Header (AAC-LC)
	header := make([]byte, 7)
	n, _ := resp.Body.Read(header)
	assert.Equal(t, 7, n)

	// ADTS sync word: 0xFFF
	assert.Equal(t, byte(0xFF), header[0])
	assert.Equal(t, byte(0xF0), header[1]&0xF0)

	// Profile: AAC-LC (2)
	profile := (header[2] >> 6) & 0x03
	assert.Equal(t, uint8(2), profile)

	// Sample rate index (e.g., 44.1kHz = 4)
	sampleRateIdx := (header[2] >> 2) & 0x0F
	assert.True(t, sampleRateIdx <= 11, "valid sample rate index")

	// Channels (1 or 2)
	channels := ((header[2] & 0x01) << 2) | ((header[3] >> 6) & 0x03)
	assert.True(t, channels >= 1 && channels <= 2, "mono or stereo")
}

func TestSafariStreamPlayback(t *testing.T) {
	server := setupMode2Server(t)
	defer server.Close()

	// Simulate continuous stream reading
	req, _ := http.NewRequest("GET", server.URL+"/stream/test", nil)
	req.Header.Set("User-Agent", "Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	defer resp.Body.Close()

	// Read 10 seconds of stream
	buffer := make([]byte, 192000) // ~192kbps * 10s
	totalRead := 0

	for totalRead < len(buffer) {
		n, err := resp.Body.Read(buffer[totalRead:])
		totalRead += n
		if err != nil {
			break
		}
	}

	assert.Greater(t, totalRead, 100000, "should have streamed data")
}
```

**Value:** Medium - Automated Safari compatibility testing
**Effort:** 4h

---

## 3.2 Adaptive Bitrate Streaming (HLS/DASH) (12h+)

### Problem
Single-Bitrate Stream → schlechte UX bei langsamer Verbindung

### Lösung
Multi-Bitrate HLS Manifest Generation

**Datei:** `internal/hls/muxer.go` (neu)

```go
package hls

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

type HLSConfig struct {
	SegmentDuration int      // Seconds per segment
	Variants        []Variant // Bitrate variants
}

type Variant struct {
	Name       string
	Bitrate    string // e.g., "3000k"
	Resolution string // e.g., "1920x1080"
}

func DefaultConfig() HLSConfig {
	return HLSConfig{
		SegmentDuration: 6,
		Variants: []Variant{
			{"1080p", "6000k", "1920x1080"},
			{"720p", "3000k", "1280x720"},
			{"480p", "1500k", "854x480"},
		},
	}
}

// GenerateHLS creates HLS segments and master playlist
func GenerateHLS(inputURL string, outputDir string, config HLSConfig) error {
	// FFmpeg command for multi-bitrate HLS
	args := []string{
		"-i", inputURL,
		"-c:v", "libx264",
		"-c:a", "aac",
		"-hls_time", fmt.Sprintf("%d", config.SegmentDuration),
		"-hls_playlist_type", "event",
		"-master_pl_name", "master.m3u8",
	}

	// Add variants
	for _, v := range config.Variants {
		variantPath := filepath.Join(outputDir, v.Name+".m3u8")
		args = append(args,
			"-b:v", v.Bitrate,
			"-s", v.Resolution,
			"-hls_segment_filename", filepath.Join(outputDir, v.Name+"_%03d.ts"),
			variantPath,
		)
	}

	cmd := exec.Command("ffmpeg", args...)
	return cmd.Run()
}
```

**Master Playlist Example:**

```m3u8
#EXTM3U
#EXT-X-VERSION:3

#EXT-X-STREAM-INF:BANDWIDTH=6000000,RESOLUTION=1920x1080
1080p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=3000000,RESOLUTION=1280x720
720p.m3u8

#EXT-X-STREAM-INF:BANDWIDTH=1500000,RESOLUTION=854x480
480p.m3u8
```

**Value:** High - Better UX for mobile/slow connections
**Effort:** 12h+ (requires FFmpeg segmenting, storage)

---

## 3.3 Cloudflare Tunnel Integration (3h)

### Problem
Port-Forwarding für externe Zugriffe unsicher

### Lösung
Cloudflare Tunnel Sidecar Container

**Datei:** `docker-compose.cloudflare.yml` (neu)

```yaml
version: '3.8'

services:
  xg2g:
    image: ghcr.io/manugh/xg2g:latest
    # ... existing config
    networks:
      - internal

  cloudflared:
    image: cloudflare/cloudflared:latest
    container_name: cloudflared
    restart: unless-stopped
    command: tunnel --no-autoupdate run --token ${CLOUDFLARE_TUNNEL_TOKEN}
    environment:
      - TUNNEL_TOKEN=${CLOUDFLARE_TUNNEL_TOKEN}
    networks:
      - internal
    depends_on:
      - xg2g

networks:
  internal:
    driver: bridge
```

**Setup Guide:**

```bash
# 1. Create Cloudflare Tunnel
cloudflared tunnel create xg2g-tunnel

# 2. Get tunnel token
cloudflared tunnel token xg2g-tunnel

# 3. Configure public hostname
cloudflared tunnel route dns xg2g-tunnel xg2g.example.com

# 4. Set token in .env
echo "CLOUDFLARE_TUNNEL_TOKEN=your-token-here" >> .env

# 5. Start
docker-compose -f docker-compose.cloudflare.yml up -d
```

**Benefits:**
- ✅ Zero Trust Access
- ✅ No Port Forwarding
- ✅ DDoS Protection
- ✅ HTTPS Automatic

**Value:** Medium - Secure external access
**Effort:** 3h

---

## 3.4 Redis-based EPG Caching (5h)

### Problem
EPG-Daten werden in Memory gecacht, nicht persistent

### Lösung
Redis als Shared Cache zwischen Restarts

**Datei:** `internal/cache/redis.go` (neu)

```go
package cache

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(addr string) *RedisCache {
	return &RedisCache{
		client: redis.NewClient(&redis.Options{
			Addr: addr,
		}),
	}
}

func (c *RedisCache) Get(key string) (any, bool) {
	ctx := context.Background()
	val, err := c.client.Get(ctx, key).Result()
	if err != nil {
		return nil, false
	}

	var result any
	if err := json.Unmarshal([]byte(val), &result); err != nil {
		return nil, false
	}

	return result, true
}

func (c *RedisCache) Set(key string, value any, ttl time.Duration) {
	ctx := context.Background()
	data, _ := json.Marshal(value)
	c.client.Set(ctx, key, data, ttl)
}
```

**Docker Compose:**

```yaml
services:
  redis:
    image: redis:7-alpine
    container_name: redis
    restart: unless-stopped
    volumes:
      - redis-data:/data
    networks:
      - monitoring

  xg2g:
    environment:
      - XG2G_CACHE_BACKEND=redis
      - XG2G_REDIS_ADDR=redis:6379
```

**Value:** Medium - Persistent cache, faster restarts
**Effort:** 5h

---

## 3.5 Authentication Dashboard (6h)

### Problem
API Token Auth ist einfach, aber kein UI für User Management

### Lösung
Web-basiertes Admin Dashboard

**Datei:** `internal/dashboard/admin.go` (neu)

```go
package dashboard

import (
	"html/template"
	"net/http"
)

type AdminDashboard struct {
	templates *template.Template
}

func NewAdminDashboard() *AdminDashboard {
	return &AdminDashboard{
		templates: template.Must(template.ParseFiles("templates/admin.html")),
	}
}

func (d *AdminDashboard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check auth
	// ...

	data := map[string]interface{}{
		"ActiveStreams": getActiveStreams(),
		"GPUUtilization": getGPUUtilization(),
		"RateLimitStats": getRateLimitStats(),
	}

	d.templates.Execute(w, data)
}
```

**HTML Template:**

```html
<!DOCTYPE html>
<html>
<head>
    <title>xg2g Admin Dashboard</title>
    <link rel="stylesheet" href="/static/admin.css">
</head>
<body>
    <h1>xg2g Production Dashboard</h1>

    <div class="metrics">
        <div class="card">
            <h2>Active Streams</h2>
            <p class="value">{{ .ActiveStreams }}</p>
        </div>

        <div class="card">
            <h2>GPU Utilization</h2>
            <p class="value">{{ .GPUUtilization }}%</p>
        </div>
    </div>
</body>
</html>
```

**Value:** Low - Nice UI, but Grafana already exists
**Effort:** 6h

---

## Summary

| Feature | Value | Effort | Priority |
|---------|-------|--------|----------|
| Safari/iOS Tests | Medium | 4h | 3 |
| Adaptive Bitrate (HLS) | High | 12h+ | 2 |
| Cloudflare Tunnel | Medium | 3h | 4 |
| Redis EPG Cache | Medium | 5h | 5 |
| Admin Dashboard | Low | 6h | 6 |

**Total Effort (all Tier 3):** ~30h

---

## Recommendation

**Skip Tier 3 initially** - Focus on Tier 1 (Production Critical) and Tier 2 (High-Value).

Only implement Tier 3 if:
- You have specific user requests
- You've completed Tier 1 + 2
- You have spare time

**Best bang-for-buck from Tier 3:**
1. **Cloudflare Tunnel** (3h) - Easy secure external access
2. **Safari/iOS Tests** (4h) - Automated quality assurance

**Skip for now:**
- HLS (complex, requires storage)
- Redis Cache (Memory cache sufficient for most)
- Admin Dashboard (Grafana already provides this)
