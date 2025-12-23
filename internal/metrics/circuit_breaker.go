// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	circuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "xg2g_circuit_breaker_state",
		Help: "Circuit breaker state by component (closed=1, half-open=1, open=1; others 0)",
	}, []string{"component", "state"})

	circuitBreakerTrips = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_circuit_breaker_trips_total",
		Help: "Total number of circuit breaker trips (transitions to open state)",
	}, []string{"component", "reason"})
)

var circuitStates = []string{"closed", "half-open", "open"}

// SetCircuitBreakerState records the active circuit breaker state for a component.
func SetCircuitBreakerState(component, state string) {
	for _, s := range circuitStates {
		value := 0.0
		if s == state {
			value = 1.0
		}
		circuitBreakerState.WithLabelValues(component, s).Set(value)
	}
}

// RecordCircuitBreakerTrip increments the trip counter when circuit breaker opens.
func RecordCircuitBreakerTrip(component, reason string) {
	circuitBreakerTrips.WithLabelValues(component, reason).Inc()
}
