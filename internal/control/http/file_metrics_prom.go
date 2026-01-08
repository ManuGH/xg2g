// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package http

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// promFileMetrics implements FileMetrics using Prometheus counters.
type promFileMetrics struct {
	deniedTotal  *prometheus.CounterVec
	allowedTotal prometheus.Counter
	cacheHits    prometheus.Counter
	cacheMisses  prometheus.Counter
}

// NewPromFileMetrics creates a Prometheus-backed FileMetrics implementation.
func NewPromFileMetrics() FileMetrics {
	return &promFileMetrics{
		deniedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: "xg2g_file_requests_denied_total",
			Help: "Number of file requests denied for security reasons",
		}, []string{"reason"}),
		allowedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "xg2g_file_requests_allowed_total",
			Help: "Number of file requests allowed",
		}),
		cacheHits: promauto.NewCounter(prometheus.CounterOpts{
			Name: "xg2g_file_cache_hits_total",
			Help: "Number of file requests served as 304 Not Modified",
		}),
		cacheMisses: promauto.NewCounter(prometheus.CounterOpts{
			Name: "xg2g_file_cache_misses_total",
			Help: "Number of file requests resulting in 200 OK (content served)",
		}),
	}
}

func (m *promFileMetrics) Denied(reason string) {
	m.deniedTotal.WithLabelValues(reason).Inc()
}

func (m *promFileMetrics) Allowed() {
	m.allowedTotal.Inc()
}

func (m *promFileMetrics) CacheHit() {
	m.cacheHits.Inc()
}

func (m *promFileMetrics) CacheMiss() {
	m.cacheMisses.Inc()
}
