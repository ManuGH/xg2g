package metrics

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	recordingsPreparingTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_recordings_preparing_total",
		Help: "Total number of recordings playback preparing responses by probe state and blocked reason",
	}, []string{"probe_state", "blocked_reason"})
)

// IncRecordingsPreparing records a preparing response with normalized labels.
// Label allowlists are intentionally strict to cap cardinality:
// probe_state ∈ {queued,in_flight,blocked,unknown}
// blocked_reason ∈ {probe_disabled,probe_backoff,remote_probe_failed,none,unknown}
// blocked_reason is forced to "none" unless probe_state is "blocked".
func IncRecordingsPreparing(probeState, blockedReason string) {
	stateLabel := normalizeRecordingProbeStateLabel(probeState)
	reasonLabel := normalizeRecordingBlockedReasonLabel(stateLabel, blockedReason)
	recordingsPreparingTotal.WithLabelValues(stateLabel, reasonLabel).Inc()
}

func normalizeRecordingProbeStateLabel(state string) string {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "queued", "in_flight", "blocked":
		return strings.ToLower(strings.TrimSpace(state))
	default:
		return "unknown"
	}
}

func normalizeRecordingBlockedReasonLabel(state, reason string) string {
	if state != "blocked" {
		return "none"
	}
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "", "none":
		return "none"
	case "probe_disabled", "probe_backoff", "remote_probe_failed":
		return strings.ToLower(strings.TrimSpace(reason))
	default:
		return "unknown"
	}
}
