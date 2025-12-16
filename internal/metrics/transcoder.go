package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// TranscoderBytesInput tracks total bytes processed by the transcoder
	TranscoderBytesInput = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_transcoder_bytes_input_total",
		Help: "Total bytes processed by transcoder",
	}, []string{"mode"})

	// TranscoderBytesOutput tracks total bytes produced by the transcoder
	TranscoderBytesOutput = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_transcoder_bytes_output_total",
		Help: "Total bytes produced by transcoder",
	}, []string{"mode"})

	// TranscoderProcessingDuration tracks duration of transcoder processing operations
	TranscoderProcessingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_transcoder_processing_duration_seconds",
		Help:    "Duration of transcoder processing operations",
		Buckets: prometheus.ExponentialBuckets(0.00001, 2.0, 15), // 10us to ~300ms
	}, []string{"mode"})

	// TranscoderErrors tracks errors during transcoding
	TranscoderErrors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_transcoder_errors_total",
		Help: "Total errors during transcoding",
	}, []string{"mode", "error_type"})
)
