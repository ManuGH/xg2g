// Package metrics provides Prometheus metrics for the xg2g admission subsystem.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

// Phase 5.3: Admission Control Metrics
// These metrics are mandatory for Soak & Load Validation.
// CTO Constraint: No cardinality explosion (no session_id, request_id in labels).

var (
	// Counters

	// AdmissionAdmitTotal counts successful admissions by priority.
	AdmissionAdmitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_admission_admit_total",
		Help: "Total number of admitted session requests, by priority.",
	}, []string{"priority"})

	// AdmissionRejectTotal counts rejected admissions by reason and priority.
	AdmissionRejectTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_admission_reject_total",
		Help: "Total number of rejected session requests, by reason and priority.",
	}, []string{"reason", "priority"})

	// PreemptTotal counts preemption events by victim and request priority.
	PreemptTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_preempt_total",
		Help: "Total number of preemption events, by victim and request priority.",
	}, []string{"victim_priority", "request_priority"})

	// PipelineSpawnTotal counts media pipeline process spawns by engine and cause.
	PipelineSpawnTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_pipeline_spawn_total",
		Help: "Total number of media pipeline process spawns, by engine and cause (admitted/rejected).",
	}, []string{"engine", "cause"})

	// UTIExitTotal counts UTI process exits by exit code category.
	UTIExitTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_uti_exit_total",
		Help: "Total number of UTI process exits, by exit code category.",
	}, []string{"code"})

	// InvariantViolationTotal counts critical invariant violations.
	InvariantViolationTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_invariant_violation_total",
		Help: "Total number of invariant violations, by rule.",
	}, []string{"rule"})
	// Gauges

	// ActiveSessions tracks current active sessions by priority.
	ActiveSessions = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_active_sessions",
		Help: "Current number of active sessions, by priority.",
	}, []string{"priority"})

	// GPUTokensInUse tracks current VAAPI tokens in use.
	GPUTokensInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_gpu_tokens_in_use",
		Help: "Current number of GPU (VAAPI) tokens in use.",
	})

	// TunersInUse tracks current tuners in use.
	TunersInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "xg2g_tuners_in_use",
		Help: "Current number of tuners in use.",
	})
)

// RecordAdmit increments the admission counter for successful admissions.
func RecordAdmit(priority string) {
	AdmissionAdmitTotal.WithLabelValues(priority).Inc()
}

// RecordReject increments the rejection counter.
func RecordReject(reason, priority string) {
	AdmissionRejectTotal.WithLabelValues(reason, priority).Inc()
}

// RecordPreempt increments the preemption counter.
func RecordPreempt(victimPriority, requestPriority string) {
	PreemptTotal.WithLabelValues(victimPriority, requestPriority).Inc()
}

// RecordPipelineSpawn increments the pipeline spawn counter.
// engine: "ffmpeg" or "mock"
// cause: "admitted" (normal) or "rejected" (invariant violation)
func RecordPipelineSpawn(engine, cause string) {
	PipelineSpawnTotal.WithLabelValues(engine, cause).Inc()
}

// RecordUTIExit increments the UTI exit counter.
func RecordUTIExit(code string) {
	UTIExitTotal.WithLabelValues(code).Inc()
}

// RecordInvariantViolation increments the invariant violation counter.
func RecordInvariantViolation(rule string) {
	InvariantViolationTotal.WithLabelValues(rule).Inc()
}

// SetActiveSessions sets the active session gauge for a priority.
func SetActiveSessions(priority string, count float64) {
	ActiveSessions.WithLabelValues(priority).Set(count)
}

// SetGPUTokensInUse sets the GPU tokens gauge.
func SetGPUTokensInUse(count float64) {
	GPUTokensInUse.Set(count)
}

// SetTunersInUse sets the tuners gauge.
func SetTunersInUse(count float64) {
	TunersInUse.Set(count)
}

// IncTunersInUse increments the tuners gauge.
func IncTunersInUse() {
	TunersInUse.Inc()
}

// DecTunersInUse decrements the tuners gauge.
func DecTunersInUse() {
	TunersInUse.Dec()
}

// GetTunersInUse returns the current value of the gauge (for testing).
func GetTunersInUse() float64 {
	var m dto.Metric
	if err := TunersInUse.Write(&m); err != nil {
		return 0
	}
	return m.GetGauge().GetValue()
}
