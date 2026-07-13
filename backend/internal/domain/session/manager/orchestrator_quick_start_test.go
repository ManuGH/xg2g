package manager

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestLiveReadySegments_KeepsThreeSegmentFloorForQuickStart(t *testing.T) {
	orch := &Orchestrator{LiveReadySegments: 2}

	if got := orch.liveReadySegments(profiles.ProfileSafari); got != 3 {
		t.Fatalf("Safari liveReadySegments = %d, want 3", got)
	}
	if got := orch.liveReadySegments(profiles.ProfileLow); got != 3 {
		t.Fatalf("bandwidth liveReadySegments = %d, want 3", got)
	}
	if got := orch.liveReadySegments(profiles.ProfileHigh); got != 2 {
		t.Fatalf("high liveReadySegments = %d, want configured 2", got)
	}
}
