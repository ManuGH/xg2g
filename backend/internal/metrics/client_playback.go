// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// clientPlaybackEventsTotal counts the playback telemetry events clients report
// about the stream on their screen. The server sees a stream it delivered
// without fault; only the client sees a picture that stopped moving or sound
// that ran dry, and a rising degraded count is where that becomes visible.
var clientPlaybackEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "xg2g_client_playback_events_total",
	Help: "Playback telemetry events reported by clients, by platform and kind (session_start, heartbeat, degraded, session_end)",
}, []string{"platform", "kind"})

// RecordClientPlaybackEvent counts one reported playback telemetry event.
// Callers pass only validated enum values, so the label set stays bounded.
func RecordClientPlaybackEvent(platform, kind string) {
	clientPlaybackEventsTotal.WithLabelValues(platform, kind).Inc()
}
