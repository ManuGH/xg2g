package worker

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
)
