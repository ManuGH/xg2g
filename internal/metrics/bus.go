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
)

// IncBusDrop records a dropped bus message for the given topic.
func IncBusDrop(topic string) {
	if topic == "" {
		topic = "unknown"
	}
	BusDropsTotal.WithLabelValues(topic).Inc()
}
