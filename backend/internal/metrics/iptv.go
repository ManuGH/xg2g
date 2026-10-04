// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Allowed endpoint enum values for v3_iptv_legacy_ingress_total.
const (
	EndpointIntents       = "intents"
	EndpointPlaybackInfo  = "playback_info"
	EndpointNowNext       = "now_next"
	EndpointTimers        = "timers"
	EndpointStreamPrepare = "stream_prepare"
	EndpointStreamLive    = "stream_live"
	EndpointStreamSmooth  = "stream_smooth"
	EndpointHousehold     = "household"
	EndpointTelemetry     = "telemetry"
	EndpointLogos         = "logos"
	EndpointOther         = "other"
)

var allowedIPTVEndpoints = map[string]struct{}{
	EndpointIntents:       {},
	EndpointPlaybackInfo:  {},
	EndpointNowNext:       {},
	EndpointTimers:        {},
	EndpointStreamPrepare: {},
	EndpointStreamLive:    {},
	EndpointStreamSmooth:  {},
	EndpointHousehold:     {},
	EndpointTelemetry:     {},
	EndpointLogos:         {},
	EndpointOther:         {},
}

var iptvLegacyIngressTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "v3_iptv_legacy_ingress_total",
	Help: "Total count of legacy raw IPTV service reference ingress requests by endpoint",
}, []string{"endpoint"})

// NormalizeIPTVEndpoint maps an endpoint string to a known enum value.
// Any unknown string is mapped to EndpointOther to prevent label cardinality explosion.
func NormalizeIPTVEndpoint(endpoint string) string {
	if _, ok := allowedIPTVEndpoints[endpoint]; ok {
		return endpoint
	}
	return EndpointOther
}

// IncIPTVLegacyIngress increments the legacy IPTV ingress counter for the specified endpoint.
func IncIPTVLegacyIngress(endpoint string) {
	iptvLegacyIngressTotal.WithLabelValues(NormalizeIPTVEndpoint(endpoint)).Inc()
}
