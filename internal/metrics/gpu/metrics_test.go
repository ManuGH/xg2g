// SPDX-License-Identifier: MIT

package gpu

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRecordTranscodeLatency(t *testing.T) {
	// Reset metrics before test
	GPUUtilization.Reset()
	TranscodeLatency.Reset()

	RecordTranscodeLatency("h264", "1920x1080", "renderD128", 0.025)

	// Verify histogram was updated
	count := testutil.CollectAndCount(TranscodeLatency)
	if count == 0 {
		t.Error("expected TranscodeLatency to have observations, got 0")
	}
}

func TestUpdateGPUUtilization(t *testing.T) {
	// Reset metrics before test
	GPUUtilization.Reset()

	UpdateGPUUtilization("renderD128", "video", 75.5)

	// Verify gauge was updated
	metric := testutil.ToFloat64(GPUUtilization.WithLabelValues("renderD128", "video"))
	if metric != 75.5 {
		t.Errorf("expected GPUUtilization=75.5, got %f", metric)
	}
}

func TestUpdateVRAMUsage(t *testing.T) {
	// Reset metrics before test
	GPUVRAMUsage.Reset()

	UpdateVRAMUsage("renderD128", 1024*1024*512) // 512 MB

	// Verify gauge was updated
	metric := testutil.ToFloat64(GPUVRAMUsage.WithLabelValues("renderD128"))
	expected := float64(1024 * 1024 * 512)
	if metric != expected {
		t.Errorf("expected GPUVRAMUsage=%f, got %f", expected, metric)
	}
}

func TestActiveStreamsIncDec(t *testing.T) {
	// Reset metrics before test
	ActiveStreamsByMode.Reset()

	// Increment twice
	IncActiveStreams("gpu")
	IncActiveStreams("gpu")

	metric := testutil.ToFloat64(ActiveStreamsByMode.WithLabelValues("gpu"))
	if metric != 2 {
		t.Errorf("expected ActiveStreamsByMode=2, got %f", metric)
	}

	// Decrement once
	DecActiveStreams("gpu")

	metric = testutil.ToFloat64(ActiveStreamsByMode.WithLabelValues("gpu"))
	if metric != 1 {
		t.Errorf("expected ActiveStreamsByMode=1 after decrement, got %f", metric)
	}
}

func TestRecordTranscodeError(t *testing.T) {
	// Reset metrics before test
	TranscodeErrors.Reset()

	RecordTranscodeError("h264", "timeout")
	RecordTranscodeError("h264", "timeout")
	RecordTranscodeError("h265", "device_busy")

	// Verify h264/timeout counter
	metric := testutil.ToFloat64(TranscodeErrors.WithLabelValues("h264", "timeout"))
	if metric != 2 {
		t.Errorf("expected TranscodeErrors(h264,timeout)=2, got %f", metric)
	}

	// Verify h265/device_busy counter
	metric = testutil.ToFloat64(TranscodeErrors.WithLabelValues("h265", "device_busy"))
	if metric != 1 {
		t.Errorf("expected TranscodeErrors(h265,device_busy)=1, got %f", metric)
	}
}

func TestRecordStreamDetectionError(t *testing.T) {
	// Reset metrics before test
	StreamDetectionErrors.Reset()

	RecordStreamDetectionError("8001", "timeout")
	RecordStreamDetectionError("8001", "timeout")
	RecordStreamDetectionError("17999", "invalid")

	// Verify 8001/timeout counter
	metric := testutil.ToFloat64(StreamDetectionErrors.WithLabelValues("8001", "timeout"))
	if metric != 2 {
		t.Errorf("expected StreamDetectionErrors(8001,timeout)=2, got %f", metric)
	}

	// Verify 17999/invalid counter
	metric = testutil.ToFloat64(StreamDetectionErrors.WithLabelValues("17999", "invalid"))
	if metric != 1 {
		t.Errorf("expected StreamDetectionErrors(17999,invalid)=1, got %f", metric)
	}
}

func TestMetricLabels(t *testing.T) {
	// Test that all metrics have correct labels
	tests := []struct {
		name         string
		metric       prometheus.Collector
		expectedDesc string
	}{
		{
			name:         "GPUUtilization",
			metric:       GPUUtilization,
			expectedDesc: "xg2g_gpu_utilization_percent",
		},
		{
			name:         "GPUVRAMUsage",
			metric:       GPUVRAMUsage,
			expectedDesc: "xg2g_gpu_vram_usage_bytes",
		},
		{
			name:         "TranscodeLatency",
			metric:       TranscodeLatency,
			expectedDesc: "xg2g_gpu_transcode_latency_seconds",
		},
		{
			name:         "ActiveStreamsByMode",
			metric:       ActiveStreamsByMode,
			expectedDesc: "xg2g_active_streams_by_mode",
		},
		{
			name:         "TranscodeErrors",
			metric:       TranscodeErrors,
			expectedDesc: "xg2g_gpu_transcode_errors_total",
		},
		{
			name:         "StreamDetectionErrors",
			metric:       StreamDetectionErrors,
			expectedDesc: "xg2g_openwebif_stream_detection_errors_total",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a registry and register the metric
			reg := prometheus.NewRegistry()
			reg.MustRegister(tt.metric)

			// Collect metrics and verify they can be gathered
			metricFamilies, err := reg.Gather()
			if err != nil {
				t.Fatalf("failed to gather metrics: %v", err)
			}

			found := false
			for _, mf := range metricFamilies {
				if mf.GetName() == tt.expectedDesc {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("expected metric %s not found", tt.expectedDesc)
			}
		})
	}
}

func BenchmarkRecordTranscodeLatency(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordTranscodeLatency("h264", "1920x1080", "renderD128", 0.025)
	}
}

func BenchmarkUpdateGPUUtilization(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		UpdateGPUUtilization("renderD128", "video", 75.5)
	}
}

func BenchmarkIncActiveStreams(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		IncActiveStreams("gpu")
	}
}

func BenchmarkRecordStreamDetectionError(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordStreamDetectionError("8001", "timeout")
	}
}
