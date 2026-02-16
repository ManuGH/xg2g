// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	BusDropsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_bus_drop_total",
		Help: "Total number of in-memory bus message drops (backpressure)",
	}, []string{"topic"})

	BusDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_bus_dropped_total",
		Help: "Total number of in-memory bus message drops by topic and reason",
	}, []string{"topic", "reason"})
)

// IncBusDrop records a dropped bus message for the given topic.
func IncBusDrop(topic string) {
	IncBusDropReason(topic, "full")
}

// IncBusDropReason records a dropped bus message with a concrete reason.
func IncBusDropReason(topic, reason string) {
	if topic == "" {
		topic = "unknown"
	}
	if reason == "" {
		reason = "unknown"
	}
	BusDropsTotal.WithLabelValues(topic).Inc()
	BusDroppedTotal.WithLabelValues(topic, reason).Inc()
}
