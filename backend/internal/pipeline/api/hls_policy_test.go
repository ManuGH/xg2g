package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
)

func TestDeriveHLSStartupPolicy_UsesNativeClientFloor(t *testing.T) {
	rec := &model.SessionRecord{
		ContextData: map[string]string{
			model.CtxKeyClientFamily: "safari_native",
		},
	}

	policy := deriveHLSStartupPolicy(rec, []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:2
#EXTINF:1.2,
seg_1.ts
#EXTINF:1.1,
seg_2.ts
#EXTINF:1.3,
seg_3.ts
`))

	assert.Equal(t, "safari_native", policy.ClientFamily)
	assert.Equal(t, 8, policy.StartupHeadroomSec)
	assert.Equal(t, "native_guarded", policy.Mode)
	assert.Equal(t, []string{"client_family_native"}, policy.Reasons)
}

func TestDeriveHLSStartupPolicy_UsesTraceEvidenceConservatively(t *testing.T) {
	rec := &model.SessionRecord{
		PlaybackTrace: &model.PlaybackTrace{
			HLS: &model.HLSAccessTrace{
				PlaylistRequestCount: 3,
				SegmentRequestCount:  0,
				LastSegmentGapMs:     9000,
			},
		},
	}

	policy := deriveHLSStartupPolicy(rec, []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:3
#EXTINF:2.0,
seg_1.ts
#EXTINF:2.0,
seg_2.ts
`))

	assert.Equal(t, 12, policy.StartupHeadroomSec)
	assert.Equal(t, "trace_conservative", policy.Mode)
	assert.Equal(t, []string{"trace_segment_gap", "trace_playlist_only"}, policy.Reasons)
}

func TestDeriveHLSStartupPolicy_DoesNotUseAdvisoryStallRiskAsPolicyTruth(t *testing.T) {
	rec := &model.SessionRecord{
		PlaybackTrace: &model.PlaybackTrace{
			HLS: &model.HLSAccessTrace{
				StallRisk: "producer_late",
			},
		},
	}

	policy := deriveHLSStartupPolicy(rec, []byte(`#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:3
#EXTINF:2.0,
seg_1.ts
#EXTINF:2.0,
seg_2.ts
`))

	assert.Equal(t, 8, policy.StartupHeadroomSec)
	assert.Equal(t, "balanced", policy.Mode)
	assert.Nil(t, policy.Reasons)
}
