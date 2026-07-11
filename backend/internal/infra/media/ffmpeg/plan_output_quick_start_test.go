package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestPlanLiveSegmentLayout_UsesQuickStartCadenceForSafari(t *testing.T) {
	adapter := &LocalAdapter{SegmentSeconds: 6, ReadySegments: 2}
	spec := ports.StreamSpec{
		Mode:    ports.ModeLive,
		Format:  ports.FormatHLS,
		Profile: ports.ProfileSpec{Name: profiles.ProfileSafari, Container: "mpegts"},
	}

	layout, err := adapter.planLiveSegmentLayout(spec)
	if err != nil {
		t.Fatalf("planLiveSegmentLayout() error = %v", err)
	}
	if layout.segmentDurationSec != 2 {
		t.Fatalf("segmentDurationSec = %d, want 2", layout.segmentDurationSec)
	}

	spec.Profile.Name = profiles.ProfileHigh
	layout, err = adapter.planLiveSegmentLayout(spec)
	if err != nil {
		t.Fatalf("planLiveSegmentLayout(high) error = %v", err)
	}
	if layout.segmentDurationSec != 6 {
		t.Fatalf("high segmentDurationSec = %d, want 6", layout.segmentDurationSec)
	}
}
