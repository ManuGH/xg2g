// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package v3

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// V3 Metrics (Phase 2 Observability Gate)
	v3IntentsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_intents_total", // Prefix aligned with xg2g_
		Help: "Total V3 intents processed by outcome and phase.",
	}, []string{"type", "mode", "outcome"})

	v3IdempotentReplayTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_idempotent_replay_total",
		Help: "Total duplicate intents suppressed via idempotency.",
	}, []string{"type"})

	v3BusPublishTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_bus_publish_total",
		Help: "Total bus events published by API.",
	}, []string{"topic", "outcome"})

	// Bouquets Observability (Hardening)
	v3BouquetsMissingTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_v3_bouquets_playlist_missing_total",
		Help: "Total requests where bouquets fell back to config due to missing M3U.",
	})

	v3BouquetsEmptyTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "xg2g_v3_bouquets_playlist_empty_total",
		Help: "Total requests where M3U exists but contains 0 bouquets.",
	})

	v3BouquetsReadErrorTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "xg2g_v3_bouquets_read_error_total",
		Help: "Total requests where bouquets failed to read due to system error.",
	}, []string{"cause"})
)

// RecordV3Intent observes the outcome of an intent request.
func RecordV3Intent(intentType, mode, outcome string) {
	v3IntentsTotal.WithLabelValues(intentType, mode, outcome).Inc()
}

// RecordV3Replay counts suppressed idempotent replays.
func RecordV3Replay(intentType string) {
	v3IdempotentReplayTotal.WithLabelValues(intentType).Inc()
}

// RecordV3Publish counts bus publication attempts.
func RecordV3Publish(topic, outcome string) {
	v3BusPublishTotal.WithLabelValues(topic, outcome).Inc()
}
