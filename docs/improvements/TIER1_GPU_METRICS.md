# Tier 1: GPU Metrics Implementation

## Ziel
GPU-spezifische Observability für Mode 3 (Hardware Transcoding)

## Problem
Aktuell keine Sichtbarkeit über:
- GPU Auslastung während Transcoding
- VRAM Usage
- Transcode Latency pro Resolution/Codec
- Aktive Streams pro Modus

## Lösung

### 1. Neue Metrics definieren

**Datei:** `internal/metrics/gpu.go` (neu)

```go
// SPDX-License-Identifier: MIT

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// GPU utilization percentage
	GPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "xg2g_gpu_utilization_percent",
			Help: "Current GPU utilization percentage",
		},
		[]string{"device", "mode"}, // device: "renderD128", mode: "video|audio"
	)

	// Transcode latency histogram
	TranscodeLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_transcode_latency_seconds",
			Help:    "Time to transcode a frame/segment",
			Buckets: prometheus.ExponentialBuckets(0.01, 2, 8), // 10ms to 2.56s
		},
		[]string{"codec", "resolution"}, // codec: "h264|h265", resolution: "1080p|720p|480p"
	)

	// VRAM usage in bytes
	VRAMUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "xg2g_gpu_vram_usage_bytes",
			Help: "VRAM usage by GPU transcoding",
		},
		[]string{"device"},
	)

	// Active streams by mode
	ActiveStreamsByMode = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "xg2g_active_streams_by_mode",
			Help: "Number of active streams per mode",
		},
		[]string{"mode"}, // "standard", "audio_proxy", "gpu"
	)

	// Stream detection errors
	StreamDetectionErrors = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_stream_detection_errors_total",
			Help: "Stream detection failures by port and error type",
		},
		[]string{"port", "error_type"}, // port: "8001|17999", error_type: "timeout|no_response|invalid"
	)
)

// RecordTranscodeLatency records the latency of a transcode operation
func RecordTranscodeLatency(codec, resolution string, latency float64) {
	TranscodeLatency.WithLabelValues(codec, resolution).Observe(latency)
}

// UpdateGPUUtilization updates the current GPU utilization
func UpdateGPUUtilization(device, mode string, percent float64) {
	GPUUtilization.WithLabelValues(device, mode).Set(percent)
}

// UpdateVRAMUsage updates the current VRAM usage
func UpdateVRAMUsage(device string, bytes int64) {
	VRAMUsage.WithLabelValues(device).Set(float64(bytes))
}

// IncActiveStreams increments active stream count for a mode
func IncActiveStreams(mode string) {
	ActiveStreamsByMode.WithLabelValues(mode).Inc()
}

// DecActiveStreams decrements active stream count for a mode
func DecActiveStreams(mode string) {
	ActiveStreamsByMode.WithLabelValues(mode).Dec()
}

// RecordStreamDetectionError records a stream detection error
func RecordStreamDetectionError(port, errorType string) {
	StreamDetectionErrors.WithLabelValues(port, errorType).Inc()
}
```

### 2. Integration in Transcoder

**Datei:** `internal/proxy/transcoder.go` (erweitern)

```go
// Füge hinzu in Transcoder.transcode():
import "github.com/ManuGH/xg2g/internal/metrics"

func (t *Transcoder) transcode(ctx context.Context, r io.Reader) (io.ReadCloser, error) {
	start := time.Now()

	// Track active stream
	mode := "audio_proxy"
	if t.config.GPUEnabled {
		mode = "gpu"
	}
	metrics.IncActiveStreams(mode)
	defer metrics.DecActiveStreams(mode)

	// ... existing transcode logic

	// Record latency (nach erstem Frame)
	latency := time.Since(start).Seconds()
	codec := "h264" // Detect from stream
	resolution := "1080p" // Detect from stream
	metrics.RecordTranscodeLatency(codec, resolution, latency)

	return transcoded, nil
}
```

### 3. GPU Stats Collection (Optional - Linux only)

**Datei:** `internal/metrics/gpu_stats.go` (neu)

```go
package metrics

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// GPUStatsCollector periodically collects GPU stats via sysfs/intel_gpu_top
type GPUStatsCollector struct {
	device string // e.g., "/dev/dri/renderD128"
}

// NewGPUStatsCollector creates a new GPU stats collector
func NewGPUStatsCollector(device string) *GPUStatsCollector {
	return &GPUStatsCollector{device: device}
}

// Start begins collecting GPU stats every 5 seconds
func (c *GPUStatsCollector) Start(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.collect()
		}
	}
}

func (c *GPUStatsCollector) collect() {
	// Intel GPU: Use intel_gpu_top or sysfs
	// AMD GPU: Use radeontop or sysfs
	// NVIDIA GPU: Use nvidia-smi

	// Example for Intel (requires intel-gpu-tools):
	cmd := exec.Command("intel_gpu_top", "-J", "-s", "1")
	output, err := cmd.Output()
	if err != nil {
		return // GPU tools not available - graceful degradation
	}

	// Parse JSON output and extract:
	// - GPU utilization percent
	// - VRAM usage bytes
	// Update metrics accordingly
}
```

### 4. Integration in Stream Detection

**Datei:** `internal/openwebif/stream_detection.go` (erweitern)

```go
import "github.com/ManuGH/xg2g/internal/metrics"

// In testEndpoint():
func (sd *StreamDetector) testEndpoint(ctx context.Context, candidate streamCandidate) bool {
	if sd.tryRequest(ctx, http.MethodHead, candidate, false) {
		return true
	}

	// Record error
	metrics.RecordStreamDetectionError(
		fmt.Sprintf("%d", candidate.Port),
		"head_failed",
	)

	// Fallback to GET
	if sd.tryRequest(ctx, http.MethodGet, candidate, true) {
		return true
	}

	metrics.RecordStreamDetectionError(
		fmt.Sprintf("%d", candidate.Port),
		"get_failed",
	)

	return false
}
```

### 5. Grafana Dashboard

**Datei:** `deploy/monitoring/grafana/dashboards/gpu-transcoding.json` (neu)

```json
{
  "dashboard": {
    "title": "xg2g GPU Transcoding",
    "panels": [
      {
        "title": "GPU Utilization",
        "targets": [
          {
            "expr": "xg2g_gpu_utilization_percent"
          }
        ]
      },
      {
        "title": "Transcode Latency",
        "targets": [
          {
            "expr": "histogram_quantile(0.95, xg2g_transcode_latency_seconds)"
          }
        ]
      },
      {
        "title": "Active Streams by Mode",
        "targets": [
          {
            "expr": "xg2g_active_streams_by_mode"
          }
        ]
      }
    ]
  }
}
```

## Testing

```go
// internal/metrics/gpu_test.go (neu)
func TestGPUMetrics(t *testing.T) {
	// Record some metrics
	metrics.RecordTranscodeLatency("h264", "1080p", 0.05)
	metrics.UpdateGPUUtilization("renderD128", "video", 75.0)

	// Verify via prometheus registry
	families, _ := prometheus.DefaultGatherer.Gather()
	// Assert metrics exist
}
```

## Rollout

1. ✅ Metrics definieren (`gpu.go`)
2. ✅ Integration in Transcoder
3. ✅ Integration in Stream Detection
4. ⚠️ Optional: GPU Stats Collector (requires tools)
5. ✅ Grafana Dashboard
6. ✅ Tests
7. ✅ Dokumentation

## Environment Variables

```bash
# Optional: Enable GPU stats collection (requires intel-gpu-tools/radeontop)
XG2G_GPU_STATS_ENABLED=true

# Interval for GPU stats collection (default: 5s)
XG2G_GPU_STATS_INTERVAL=5s
```

## Expected Metrics Output

```
# HELP xg2g_gpu_utilization_percent Current GPU utilization percentage
# TYPE xg2g_gpu_utilization_percent gauge
xg2g_gpu_utilization_percent{device="renderD128",mode="video"} 75.0

# HELP xg2g_transcode_latency_seconds Time to transcode a frame/segment
# TYPE xg2g_transcode_latency_seconds histogram
xg2g_transcode_latency_seconds_bucket{codec="h264",resolution="1080p",le="0.01"} 0
xg2g_transcode_latency_seconds_bucket{codec="h264",resolution="1080p",le="0.02"} 5
xg2g_transcode_latency_seconds_bucket{codec="h264",resolution="1080p",le="0.04"} 12
xg2g_transcode_latency_seconds_sum{codec="h264",resolution="1080p"} 0.85
xg2g_transcode_latency_seconds_count{codec="h264",resolution="1080p"} 20

# HELP xg2g_active_streams_by_mode Number of active streams per mode
# TYPE xg2g_active_streams_by_mode gauge
xg2g_active_streams_by_mode{mode="standard"} 5
xg2g_active_streams_by_mode{mode="audio_proxy"} 2
xg2g_active_streams_by_mode{mode="gpu"} 3

# HELP xg2g_stream_detection_errors_total Stream detection failures
# TYPE xg2g_stream_detection_errors_total counter
xg2g_stream_detection_errors_total{port="8001",error_type="timeout"} 2
xg2g_stream_detection_errors_total{port="17999",error_type="no_response"} 1
```

## Success Criteria

- ✅ GPU utilization visible in Grafana
- ✅ P95 transcode latency < 50ms für 1080p
- ✅ Stream detection errors < 1% aller Requests
- ✅ Active streams per mode korrekt gezählt
