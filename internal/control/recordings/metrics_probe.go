package recordings

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	probeResultSuccess      = "success"
	probeResultProbeError   = "probe_error"
	probeResultPersistError = "persist_error"
	probeResultCanceled     = "canceled"
	probeResultTimeout      = "timeout"
)

const (
	probeBlockedReasonProbeError   = "probe_error"
	probeBlockedReasonPersistError = "persist_error"
	probeBlockedReasonCooldown     = "cooldown"
)

var (
	recordingsProbeStartedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_recordings_probe_started_total",
		Help: "Number of background recording probes started.",
	})

	recordingsProbeFinishedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_recordings_probe_finished_total",
		Help: "Number of finished background recording probes by result.",
	}, []string{"result"})

	recordingsProbeDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "xg2g_recordings_probe_duration_seconds",
		Help: "Background recording probe runtime in seconds by result.",
	}, []string{"result"})

	recordingsProbeDedupedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_recordings_probe_deduped_total",
		Help: "Number of probe attempts deduplicated by singleflight.",
	})

	recordingsProbeBlockedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_recordings_probe_blocked_total",
		Help: "Number of probe attempts resulting in blocked/cooldown state by reason.",
	}, []string{"reason"})

	recordingsProbeInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_recordings_probe_inflight",
		Help: "Number of in-flight background probes in recordings subsystem.",
	})
)
