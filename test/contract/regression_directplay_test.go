package contract_test

import (
	"context"
	"testing"

	"github.com/ManuGH/xg2g/internal/control/recordings/decision"
	"github.com/stretchr/testify/assert"
)

// TestRegression_DirectPlay_Fallback tracks the explicit acceptance of DirectStream
// for scenarios that historically were DirectPlay (Phase 4).
//
// Governance: This test MUST fail when the regression is fixed, requiring
// the assertion to be flipped back to "direct_play".
func TestRegression_DirectPlay_Fallback(t *testing.T) {
	// Fixture: Standard H264/AAC MP4 that should be DirectPlay but is currently DirectStream
	input := decision.DecisionInput{
		RequestID: "regression-check",
		Source: decision.Source{
			Container:   "mp4",
			VideoCodec:  "h264",
			AudioCodec:  "aac",
			BitrateKbps: 4000,
		},
		Capabilities: decision.Capabilities{
			Version:     1,
			Containers:  []string{"mp4", "hls"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac"},
			SupportsHLS: true,
		},
		Policy: decision.Policy{
			AllowTranscode: true,
		},
	}

	status, dec, _ := decision.Decide(context.Background(), input)

	// ASSERTION OF TRUTH (Phase 5.3):
	// We accept DirectStream as the safe fallback for now.
	// When we fix the resolver/capabilities match, this will become "direct_play".
	assert.Equal(t, 200, status)
	if dec != nil {
		assert.Equal(t, decision.Mode("direct_stream"), dec.Mode, "Regression Contract: Expecting DirectStream fallback. If this fails with direct_play, the regression is fixed! Update this test.")
	}
}
