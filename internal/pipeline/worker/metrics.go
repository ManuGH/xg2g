package worker

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/ManuGH/xg2g/internal/pipeline/model"
)

var (
	// Golden Signal: Lifecycle / Capacity
	sessionEndTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_session_end_total",
			Help: "Total number of finalized sessions by reason and profile.",
		},
		[]string{"reason", "profile", "mode"},
	)

	// Golden Signal: Startup Latency (Availability)
	readyDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_ready_duration_seconds",
			Help:    "Time taken for tuner/transcoder readiness.",
			Buckets: []float64{0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		},
		[]string{"outcome", "mode"}, // outcome: success, failure
	)

	readyOutcomeTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_ready_outcome_total",
			Help: "Detailed outcome of readiness checks.",
		},
		[]string{"outcome", "mode"}, // outcome: success, timeout, wrong_ref, not_locked, upstream, canceled, other
	)

	timeToFirstPlaylist = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_time_to_first_playlist_seconds",
			Help:    "Time from session start to playlist readiness.",
			Buckets: []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13, 21},
		},
		[]string{"profile", "mode"},
	)

	timeToFirstSegment = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xg2g_v3_time_to_first_segment_seconds",
			Help:    "Time from session start to first media segment readiness.",
			Buckets: []float64{0.5, 1, 2, 3, 5, 8, 13, 21},
		},
		[]string{"profile", "mode"},
	)

	// Golden Signal: Resource Pressure
	tunerBusyTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_tuner_busy_total",
			Help: "Total times tuner lease acquisition failed due to all slots busy.",
		},
		[]string{"mode"},
	)

	jobsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_jobs_total",
			Help: "Total worker jobs processed (Legacy)",
		},
		[]string{"result", "mode"},
	)

	fsmTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_fsm_transitions_total",
			Help: "FSM state transitions",
		},
		[]string{"state_from", "state_to", "mode"},
	)

	leaseLostTotalLegacy = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "v3_worker_lease_lost_total",
			Help: "Total leases lost during heartbeat",
		},
		[]string{"mode"},
	)

	sessionStartsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_session_starts_total",
			Help: "Total session start outcomes by result and reason class.",
		},
		[]string{"result", "reason_class", "profile", "mode"},
	)

	capacityRejectionsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xg2g_v3_capacity_rejections_total",
			Help: "Total session start rejections due to capacity constraints.",
		},
		[]string{"reason", "profile", "mode"},
	)
)

func observeTTFP(profile, mode string, start time.Time) {
	timeToFirstPlaylist.WithLabelValues(profile, mode).Observe(time.Since(start).Seconds())
}

func observeTTFS(profile, mode string, start time.Time) {
	timeToFirstSegment.WithLabelValues(profile, mode).Observe(time.Since(start).Seconds())
}

func recordSessionStartOutcome(result string, reason model.ReasonCode, profile, mode string) {
	if reason == model.RLeaseBusy {
		capacityRejectionsTotal.WithLabelValues(string(reason), profile, mode).Inc()
		return
	}
	sessionStartsTotal.WithLabelValues(result, reasonClass(reason), profile, mode).Inc()
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
	case model.RFFmpegStartFailed, model.RProcessEnded:
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
