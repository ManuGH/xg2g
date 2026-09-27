package metrics

import (
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	playbackSchemaLive      = "live"
	playbackSchemaRecording = "recording"

	playbackModeHLS       = "hls"
	playbackModeNativeHLS = "native_hls"
	playbackModeHLSJS     = "hlsjs"
	playbackModeMP4       = "mp4"
	playbackModeUnknown   = "unknown"

	playbackOutcomeOK      = "ok"
	playbackOutcomeFailed  = "failed"
	playbackOutcomeAborted = "aborted"

	playbackRebufferMinor = "minor"
	playbackRebufferMajor = "major"

	playbackStagePlaybackInfo = "playback_info"
	playbackStageIntent       = "intent"
	playbackStagePlaylist     = "playlist"
	playbackStageSegment      = "segment"
	playbackStageStream       = "stream"

	playbackCodeUnknown = "UNKNOWN"
)

var (
	playbackTTFFSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_playback_ttff_seconds",
		Help:    "Playback TTFF (time to first frame/segment) from start intent to first successful media response",
		Buckets: []float64{0.25, 0.5, 1, 2, 3, 4, 5, 8, 13, 20, 30, 45, 60},
	}, []string{"schema", "mode", "outcome"})

	playbackRebufferTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_playback_rebuffer_total",
		Help: "Server-side playback rebuffer proxy events by schema/mode/severity",
	}, []string{"schema", "mode", "severity"})

	playbackErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_playback_error_total",
		Help: "Playback errors by schema/stage/code",
	}, []string{"schema", "stage", "code"})

	playbackStartTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_playback_start_total",
		Help: "Playback start attempts by schema/mode",
	}, []string{"schema", "mode"})

	clientPlaybackTelemetryReports = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_client_playback_telemetry_reports_total",
		Help: "Client playback telemetry snapshots received by bounded platform and outcome labels.",
	}, []string{"platform", "outcome"})

	clientPlaybackIngestGapSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_client_playback_ingest_gap_seconds",
		Help:    "Largest transport data-arrival gap reported in client playback snapshots.",
		Buckets: []float64{0.1, 0.25, 0.5, 0.6, 1, 1.5, 2, 3, 5, 10, 30},
	}, []string{"platform"})

	clientPlaybackDecodedFPS = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_client_playback_decoded_fps",
		Help:    "Decoded frames per second reported by playback clients.",
		Buckets: []float64{0, 5, 10, 15, 20, 24, 25, 30, 50, 60, 120, 240},
	}, []string{"platform"})

	clientPlaybackAudioLeadSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "xg2g_client_playback_audio_lead_seconds",
		Help:    "Audio buffer lead reported by playback clients.",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 120},
	}, []string{"platform"})

	clientPlaybackErrorDeltaTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_client_playback_error_delta_total",
		Help: "Continuity and decoder error increments reported by playback clients.",
	}, []string{"platform", "kind"})
)

// RecordClientPlaybackTelemetry records a validated snapshot without session or
// device labels, keeping cardinality bounded as sessions and clients change.
func RecordClientPlaybackTelemetry(platform, outcome string, decodedFPS, audioLeadMs, maxIngestGapMs float64, continuityDelta, decodeDelta int) {
	switch platform {
	case "ios", "tvos", "android", "web":
	default:
		platform = "unknown"
	}
	switch outcome {
	case "accepted", "invalid", "not_found", "closed", "unavailable":
	default:
		outcome = "unknown"
	}
	clientPlaybackTelemetryReports.WithLabelValues(platform, outcome).Inc()
	if outcome != "accepted" {
		return
	}
	clientPlaybackIngestGapSeconds.WithLabelValues(platform).Observe(maxIngestGapMs / 1_000)
	clientPlaybackDecodedFPS.WithLabelValues(platform).Observe(decodedFPS)
	clientPlaybackAudioLeadSeconds.WithLabelValues(platform).Observe(audioLeadMs / 1_000)
	clientPlaybackErrorDeltaTotal.WithLabelValues(platform, "continuity").Add(float64(continuityDelta))
	clientPlaybackErrorDeltaTotal.WithLabelValues(platform, "decode").Add(float64(decodeDelta))
}

// IncPlaybackStart increments playback starts with strict low-cardinality labels.
func IncPlaybackStart(schema, mode string) {
	playbackStartTotal.WithLabelValues(
		normalizePlaybackSchemaLabel(schema),
		normalizePlaybackModeLabel(mode),
	).Inc()
}

// ObservePlaybackTTFF records TTFF in seconds with strict low-cardinality labels.
func ObservePlaybackTTFF(schema, mode, outcome string, seconds float64) {
	playbackTTFFSeconds.WithLabelValues(
		normalizePlaybackSchemaLabel(schema),
		normalizePlaybackModeLabel(mode),
		normalizePlaybackOutcomeLabel(outcome),
	).Observe(seconds)
}

// IncPlaybackRebuffer increments server-side rebuffer proxy counters.
func IncPlaybackRebuffer(schema, mode, severity string) {
	playbackRebufferTotal.WithLabelValues(
		normalizePlaybackSchemaLabel(schema),
		normalizePlaybackModeLabel(mode),
		normalizePlaybackSeverityLabel(severity),
	).Inc()
}

// IncPlaybackError increments playback error counters with stable code allowlists.
func IncPlaybackError(schema, stage, code string) {
	playbackErrorTotal.WithLabelValues(
		normalizePlaybackSchemaLabel(schema),
		normalizePlaybackStageLabel(stage),
		normalizePlaybackCodeLabel(code),
	).Inc()
}

func normalizePlaybackSchemaLabel(schema string) string {
	switch strings.ToLower(strings.TrimSpace(schema)) {
	case playbackSchemaLive, playbackSchemaRecording:
		return strings.ToLower(strings.TrimSpace(schema))
	default:
		return "unknown"
	}
}

func normalizePlaybackModeLabel(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case playbackModeHLS, playbackModeNativeHLS, playbackModeHLSJS, playbackModeMP4:
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return playbackModeUnknown
	}
}

func normalizePlaybackOutcomeLabel(outcome string) string {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case playbackOutcomeOK, playbackOutcomeFailed, playbackOutcomeAborted:
		return strings.ToLower(strings.TrimSpace(outcome))
	default:
		return "unknown"
	}
}

func normalizePlaybackSeverityLabel(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case playbackRebufferMinor, playbackRebufferMajor:
		return strings.ToLower(strings.TrimSpace(severity))
	default:
		return "unknown"
	}
}

func normalizePlaybackStageLabel(stage string) string {
	switch strings.ToLower(strings.TrimSpace(stage)) {
	case playbackStagePlaybackInfo, playbackStageIntent, playbackStagePlaylist, playbackStageSegment, playbackStageStream:
		return strings.ToLower(strings.TrimSpace(stage))
	default:
		return "unknown"
	}
}

func normalizePlaybackCodeLabel(code string) string {
	clean := strings.ToUpper(strings.TrimSpace(code))
	switch clean {
	case "RECORDING_PREPARING",
		"UPSTREAM_UNAVAILABLE",
		"CLAIM_MISMATCH",
		"INVALID_INPUT",
		"INVALID_CAPABILITIES",
		"UNAUTHORIZED",
		"FORBIDDEN",
		"NOT_FOUND",
		"REMOTE_PROBE_UNSUPPORTED",
		"DECISION_AMBIGUOUS",
		"ATTESTATION_UNAVAILABLE",
		"ADMISSION_SESSIONS_FULL",
		"ADMISSION_TRANSCODES_FULL",
		"ADMISSION_NO_TUNERS",
		"ADMISSION_ENGINE_DISABLED",
		"ADMISSION_STATE_UNKNOWN",
		"SERVICE_UNAVAILABLE",
		"INTERNAL_ERROR",
		"SESSION_NOT_FOUND",
		"SESSION_GONE",
		"V3_UNAVAILABLE",
		"UNAVAILABLE":
		return clean
	default:
		// Preserve numeric HTTP code if passed as string by a caller.
		if _, err := strconv.Atoi(clean); err == nil {
			return clean
		}
		return playbackCodeUnknown
	}
}
