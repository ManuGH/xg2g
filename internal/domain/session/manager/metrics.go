package manager

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
)

var (
	// Golden Signal: Lifecycle / Capacity
	sessionEndTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_session_end_total",
			Help: "Total number of finalized sessions by reason and profile.",
		},
		[]string{"reason", "profile"},
	)

	timeToFirstPlaylist = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_time_to_first_playlist_seconds",
			Help:    "Time from session start to playlist readiness.",
			Buckets: []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13, 21},
		},
		[]string{"profile"},
	)

	// Golden Signal: Resource Pressure
	tunerBusyTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_tuner_busy_total",
			Help: "Total times tuner lease acquisition failed due to all slots busy.",
		},
		[]string{},
	)

	jobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_jobs_total",
			Help: "Total worker jobs processed (Legacy)",
		},
		[]string{"result"},
	)

	fsmTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_fsm_transitions_total",
			Help: "FSM state transitions",
		},
		[]string{"state_from", "state_to"},
	)

	leaseLostTotalLegacy = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_lease_lost_total",
			Help: "Total leases lost during heartbeat",
		},
		[]string{},
	)

	sessionStartsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_session_starts_total",
			Help: "Total session start outcomes by result and reason class.",
		},
		[]string{"result", "reason_class", "profile"},
	)

	capacityRejectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_capacity_rejections_total",
			Help: "Total session start rejections due to capacity constraints.",
		},
		[]string{"reason", "profile"},
	)

	// ADR-009: Session Lease Metrics
	sessionsLeaseExpiredTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_sessions_lease_expired_total",
			Help: "Total sessions expired due to lease timeout",
		},
		[]string{"previous_state"},
	)
)

func observeTTFP(profile string, start time.Time) {
	timeToFirstPlaylist.WithLabelValues(profile).Observe(time.Since(start).Seconds())
}

func recordSessionStartOutcome(result string, reason model.ReasonCode, profile string) {
	if reason == model.RLeaseBusy {
		capacityRejectionsTotal.WithLabelValues(string(reason), profile).Inc()
		return
	}
	sessionStartsTotal.WithLabelValues(result, reasonClass(reason), profile).Inc()
}

func reasonClass(reason model.ReasonCode) string {
	switch reason {
	case model.RNone:
		return "none"
	case model.RLeaseBusy:
		return "lease_busy"
	case model.RTuneTimeout:
		return "timeout"
	case model.RTuneFailed:
		return "tune_failed"
	case model.RPipelineStartFailed, model.RProcessEnded:
		return "ffmpeg_failed"
	case model.RPackagerFailed:
		return "packager_failed"
	case model.RBadRequest:
		return "bad_request"
	case model.RNotFound:
		return "not_found"
	case model.RCancelled, model.RClientStop, model.RIdleTimeout:
		return "canceled"
	case model.RLeaseExpired, model.RInvariantViolation:
		return "internal"
	default:
		return "unknown"
	}
}
