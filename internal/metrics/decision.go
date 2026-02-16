package metrics

import (
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	decisionTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_decision_total",
		Help: "Total number of codec planning decisions by path, codec, hw usage, reason, and profile",
	}, []string{"path", "output_codec", "use_hwaccel", "reason", "profile"})
)

// RecordDecisionSummary records one decision planning outcome.
func RecordDecisionSummary(profile, path, outputCodec string, useHWAccel bool, reason string) {
	decisionTotal.WithLabelValues(
		normalizeDecisionPathLabel(path),
		normalizeDecisionCodecLabel(outputCodec),
		strconv.FormatBool(useHWAccel),
		normalizeDecisionReasonLabel(reason),
		normalizeDecisionProfileLabel(profile),
	).Inc()
}

func normalizeDecisionPathLabel(path string) string {
	switch strings.ToLower(strings.TrimSpace(path)) {
	case "direct", "remux", "transcode_cpu", "transcode_hw", "reject":
		return strings.ToLower(strings.TrimSpace(path))
	default:
		return "unknown"
	}
}

func normalizeDecisionCodecLabel(codec string) string {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "h264", "hevc", "av1":
		return strings.ToLower(strings.TrimSpace(codec))
	default:
		return "unknown"
	}
}

func normalizeDecisionReasonLabel(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "direct_play_supported", "remux_required", "profile_preference", "hw_codec_unavailable", "profile_constraint", "cost_cpu_preferred", "codec_selected", "no_compatible_codec":
		return strings.ToLower(strings.TrimSpace(reason))
	default:
		return "unknown"
	}
}

func normalizeDecisionProfileLabel(profile string) string {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "generic", "safari", "safari_dvr", "safari_hevc", "safari_hevc_hw", "safari_hevc_hw_ll", "av1_hw", "av1_required", "llhls", "auto":
		return strings.ToLower(strings.TrimSpace(profile))
	default:
		return "unknown"
	}
}
