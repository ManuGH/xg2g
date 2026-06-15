package recordings

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/config"
	"github.com/ManuGH/xg2g/internal/control/vod"
)

// TestTruthProvider_ActiveBuildGate_KeysByVariantDir is M25's RED control. The active-build
// gate must look up the vodManager under the SAME cache dir the build registers under — the
// DEFAULT VARIANT dir (DefaultRecordingVariantHash, see service.go) — NOT RecordingCacheDir
// (== variant ""). The mock holds a Building job ONLY under the variant dir; with the fix the
// gate finds it and reports Preparing, without the fix it looks up the empty-variant dir,
// misses, and the gate stays dead (silently never firing). Deterministic, no timing.
func TestTruthProvider_ActiveBuildGate_KeysByVariantDir(t *testing.T) {
	const hlsRoot = "/tmp/xg2g-m25-test-hls"
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/media/hdd/movie/building.ts"

	cfg := &config.AppConfig{
		Enigma2:                 config.Enigma2Settings{BaseURL: "http://receiver:80", StreamPort: 8001},
		RecordingPlaybackPolicy: config.PlaybackPolicyReceiverOnly,
	}
	cfg.HLS.Root = hlsRoot

	// The build registers its job under the variant dir, not the empty-variant dir.
	variantDir, err := RecordingVariantCacheDir(hlsRoot, serviceRef, DefaultRecordingVariantHash())
	require.NoError(t, err)
	emptyDir, err := RecordingCacheDir(hlsRoot, serviceRef)
	require.NoError(t, err)
	require.NotEqual(t, variantDir, emptyDir, "precondition: the default variant dir must differ from the empty-variant dir, else the bug is unobservable")

	mgr := &mockManager{
		data: map[string]vod.Metadata{},
		jobs: map[string]*vod.JobStatus{
			variantDir: {State: vod.JobStateBuilding},
		},
	}

	tp, err := newTruthProvider(cfg, mgr, ResolverOptions{})
	require.NoError(t, err)

	outcome := tp.GetMediaTruthOutcome(context.Background(), serviceRef)
	require.Equal(t, TruthStatusPreparing, outcome.Status,
		"active build (job under the variant dir) must be reported as Preparing; if this is not Preparing the gate looked up the wrong (empty-variant) key")
	require.Contains(t, outcome.Reasons, ReasonProbeInFlight)
}
