package api

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/stretchr/testify/assert"
)

func TestDeriveSessionPlaybackHealth_Healthy(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 6,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  5,
			LastSegmentAtUnix:    1700000002,
			LastSegmentName:      "seg_000010.ts",
			LastSegmentGapMs:     1200,
			LatestSegmentLagMs:   900,
			StallRisk:            "producer_late",
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthHealthy, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonSegmentProgressObserved,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_EdgeFragile(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 5,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  4,
			LastSegmentAtUnix:    1700000002,
			LastSegmentName:      "seg_000014.ts",
			LastSegmentGapMs:     3500,
			LatestSegmentLagMs:   2800,
			StartupHeadroomSec:   8,
			StallRisk:            "low",
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "safari_native",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthEdgeFragile, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonSegmentProgressObserved,
		sessionHealthReasonSegmentGapObserved,
		sessionHealthReasonLowStartupHeadroom,
		sessionHealthReasonClientFamilyWebkit,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_ProducerSlow(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 5,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  4,
			LastSegmentAtUnix:    1700000002,
			LastSegmentName:      "seg_000020.ts",
			LastSegmentGapMs:     2200,
			LatestSegmentLagMs:   11000,
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthProducerSlow, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonSegmentProgressObserved,
		sessionHealthReasonHighSegmentLag,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_SegmentStale(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 4,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  3,
			LastSegmentAtUnix:    1700000002,
			LastSegmentName:      "seg_000021.ts",
			LastSegmentGapMs:     9500,
			LatestSegmentLagMs:   3000,
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthSegmentStale, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonSegmentProgressObserved,
		sessionHealthReasonSegmentGapObserved,
		sessionHealthReasonLastSegmentStale,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_ConsumerStalled(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 4,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  0,
			LatestSegmentLagMs:   1500,
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthConsumerStalled, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonPlaylistAdvancingNoSegments,
		sessionHealthReasonConsumerProgressMissing,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_InsufficientEvidence(t *testing.T) {
	trace := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 1,
			LastPlaylistAtUnix:   1700000001,
			StartupHeadroomSec:   8,
			StallRisk:            "segment_stale",
		},
	}

	got := DeriveSessionPlaybackHealth(trace, SessionPlaybackHealthContext{
		ClientFamily: "safari_native",
		SessionMode:  "live",
	})

	assert.Equal(t, SessionPlaybackHealthInsufficientEvidence, got.Health)
	assert.Equal(t, []string{
		sessionHealthReasonPlaylistProgressObserved,
		sessionHealthReasonLowStartupHeadroom,
		sessionHealthReasonClientFamilyWebkit,
		sessionHealthReasonInsufficientTraceDepth,
	}, got.ReasonCodes)
}

func TestDeriveSessionPlaybackHealth_StallHintDoesNotAffectClassification(t *testing.T) {
	a := &model.PlaybackTrace{
		HLS: &model.HLSAccessTrace{
			PlaylistRequestCount: 6,
			LastPlaylistAtUnix:   1700000001,
			SegmentRequestCount:  5,
			LastSegmentAtUnix:    1700000002,
			LastSegmentName:      "seg_000010.ts",
			LastSegmentGapMs:     1200,
			LatestSegmentLagMs:   900,
			StallRisk:            "low",
		},
	}
	b := a.Clone()
	b.HLS.StallRisk = "producer_late"

	gotA := DeriveSessionPlaybackHealth(a, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})
	gotB := DeriveSessionPlaybackHealth(b, SessionPlaybackHealthContext{
		ClientFamily: "chromium_hlsjs",
		SessionMode:  "live",
	})

	assert.Equal(t, gotA, gotB)
}
