// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIPTVLegacyIngress_Normalization(t *testing.T) {
	expectedEndpoints := []string{
		EndpointIntents,
		EndpointPlaybackInfo,
		EndpointNowNext,
		EndpointTimers,
		EndpointStreamPrepare,
		EndpointStreamLive,
		EndpointStreamSmooth,
		EndpointHousehold,
		EndpointTelemetry,
		EndpointLogos,
	}

	for _, ep := range expectedEndpoints {
		assert.Equal(t, ep, NormalizeIPTVEndpoint(ep), "valid endpoint should normalize to itself: %s", ep)
	}

	assert.Equal(t, EndpointOther, NormalizeIPTVEndpoint("other"))
	assert.Equal(t, EndpointOther, NormalizeIPTVEndpoint("unknown"))
	assert.Equal(t, EndpointOther, NormalizeIPTVEndpoint("/api/v3/intents"))
	assert.Equal(t, EndpointOther, NormalizeIPTVEndpoint("4097:0:1:..."))
	assert.Equal(t, EndpointOther, NormalizeIPTVEndpoint(""))
}

func TestIPTVLegacyIngress_MetricCounter(t *testing.T) {
	initialOther := testutil.ToFloat64(iptvLegacyIngressTotal.WithLabelValues(EndpointOther))
	initialIntents := testutil.ToFloat64(iptvLegacyIngressTotal.WithLabelValues(EndpointIntents))

	IncIPTVLegacyIngress(EndpointIntents)
	assert.Equal(t, initialIntents+1, testutil.ToFloat64(iptvLegacyIngressTotal.WithLabelValues(EndpointIntents)))

	// Unknown endpoint must increment "other"
	IncIPTVLegacyIngress("arbitrary_unknown_caller_string")
	assert.Equal(t, initialOther+1, testutil.ToFloat64(iptvLegacyIngressTotal.WithLabelValues(EndpointOther)))
}

func TestIPTVLegacyIngress_LabelSetEnumStrict(t *testing.T) {
	// Assert the metric descriptor uses "endpoint" as its label
	descCh := make(chan *prometheus.Desc, 1)
	iptvLegacyIngressTotal.Describe(descCh)
	desc := <-descCh
	require.NotNil(t, desc)

	descStr := desc.String()
	assert.Contains(t, descStr, "v3_iptv_legacy_ingress_total")
	assert.Contains(t, descStr, "variableLabels: {endpoint}")

	// Ensure the allowed enum map matches the 10 defined enum values exactly
	expectedSet := map[string]struct{}{
		"intents":        {},
		"playback_info":  {},
		"now_next":       {},
		"timers":         {},
		"stream_prepare": {},
		"stream_live":    {},
		"stream_smooth":  {},
		"household":      {},
		"telemetry":      {},
		"logos":          {},
		"other":          {},
	}

	assert.Equal(t, len(expectedSet), len(allowedIPTVEndpoints))
	for k := range expectedSet {
		_, ok := allowedIPTVEndpoints[k]
		assert.True(t, ok, "expected %s in allowedIPTVEndpoints", k)
	}
}

// TestIPTVLegacyIngress_LabelValueSanity verifies that passing raw URLs or high-cardinality values
// never ends up as a Prometheus label value, and always maps strictly to the enum.
func TestIPTVLegacyIngress_LabelValueSanity(t *testing.T) {
	testCases := []string{
		"http://provider.invalid/channel",
		"4097:0:1:0:0:0:0:0:0:0:http%3a//...",
		"user:pass@host",
		"../../../../etc/passwd",
		"very_long_random_client_string_1234567890",
	}

	for _, tc := range testCases {
		norm := NormalizeIPTVEndpoint(tc)
		assert.Equal(t, EndpointOther, norm)
		assert.False(t, strings.Contains(norm, tc))
	}
}
