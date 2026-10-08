package manager

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckPlaylistReadyAt_LongGOP_PassesWithFewerSegments verifies that when source
// GOPs are coarse (e.g. 9.8s IPTV segments), 2 segments providing 19.6s of media
// satisfy readiness even if LiveReadySegments is configured to 4.
func TestCheckPlaylistReadyAt_LongGOP_PassesWithFewerSegments(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")
	content := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:10
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:9.800000,
seg_000000.m4s
#EXTINF:9.800000,
seg_000001.m4s
`
	require.NoError(t, os.WriteFile(playlistPath, []byte(content), 0o600))
	for _, name := range []string{"seg_000000.m4s", "seg_000001.m4s"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("dummy-media-bytes"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, model.SessionFirstFrameMarkerFilename), []byte("marker"), 0o600))

	// Configured for 4 segments of 2s = 8.0s target duration
	orch := &Orchestrator{
		LiveReadySegments:  4,
		LiveSegmentSeconds: 2,
	}
	ttfpRecorded := false

	ready, reason, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "auto", time.Now())
	require.NoError(t, err)
	assert.True(t, ready, "2 segments totaling 19.6s must pass readiness against 8.0s target")
	assert.Empty(t, reason)
	assert.True(t, ttfpRecorded)
}

// TestCheckPlaylistReadyAt_ShortGOP_WaitsForPlayableDuration verifies that when source
// GOPs are short (1.0s segments), reaching segment count alone is not enough if total
// playable duration is under target.
func TestCheckPlaylistReadyAt_ShortGOP_WaitsForPlayableDuration(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")

	// 3 segments of 1.0s each = 3.0s total (target is 3 * 2s = 6.0s)
	content3 := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:2
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:1.000000,
seg_000000.m4s
#EXTINF:1.000000,
seg_000001.m4s
#EXTINF:1.000000,
seg_000002.m4s
`
	require.NoError(t, os.WriteFile(playlistPath, []byte(content3), 0o600))
	for _, name := range []string{"seg_000000.m4s", "seg_000001.m4s", "seg_000002.m4s"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("dummy-media-bytes"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, model.SessionFirstFrameMarkerFilename), []byte("marker"), 0o600))

	orch := &Orchestrator{
		LiveReadySegments:  3,
		LiveSegmentSeconds: 2,
	}
	ttfpRecorded := false

	ready, reason, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "auto", time.Now())
	require.NoError(t, err)
	assert.False(t, ready, "3 segments with only 3.0s total must stay not ready against 6.0s target")
	assert.Contains(t, reason, "insufficient playable media duration")

	// Now add 3 more segments (total 6 segments of 1.0s = 6.0s)
	content6 := content3 + `#EXTINF:1.000000,
seg_000003.m4s
#EXTINF:1.000000,
seg_000004.m4s
#EXTINF:1.000000,
seg_000005.m4s
`
	require.NoError(t, os.WriteFile(playlistPath, []byte(content6), 0o600))
	for _, name := range []string{"seg_000003.m4s", "seg_000004.m4s", "seg_000005.m4s"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("dummy-media-bytes"), 0o600))
	}

	ready, reason, err = orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "auto", time.Now())
	require.NoError(t, err)
	assert.True(t, ready, "6 segments totaling 6.0s must pass readiness")
	assert.Empty(t, reason)
}

// TestCheckPlaylistReadyAt_VariableGOP_CalculatesExactDuration verifies that irregular
// keyframe spacing sums exact EXTINF values and releases as soon as duration is reached.
func TestCheckPlaylistReadyAt_VariableGOP_CalculatesExactDuration(t *testing.T) {
	dir := t.TempDir()
	playlistPath := filepath.Join(dir, "index.m3u8")

	content := `#EXTM3U
#EXT-X-VERSION:6
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:1.500000,
seg_000000.ts
#EXTINF:4.500000,
seg_000001.ts
`
	require.NoError(t, os.WriteFile(playlistPath, []byte(content), 0o600))
	for _, name := range []string{"seg_000000.ts", "seg_000001.ts"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("dummy-media-bytes"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, model.SessionFirstFrameMarkerFilename), []byte("marker"), 0o600))

	// Configured for 3 segments of 2s = 6.0s target duration
	orch := &Orchestrator{
		LiveReadySegments:  3,
		LiveSegmentSeconds: 2,
	}
	ttfpRecorded := false

	ready, reason, err := orch.checkPlaylistReadyAt(playlistPath, false, &ttfpRecorded, "high", time.Now())
	require.NoError(t, err)
	assert.True(t, ready, "1.5s + 4.5s = 6.0s must pass against 6.0s target with 2 segments")
	assert.Empty(t, reason)
}

// TestClassifySessionSource verifies separate classification of source origin (DVB vs IPTV)
// and transport format (MPEG-TS vs HLS).
func TestClassifySessionSource(t *testing.T) {
	tests := []struct {
		name       string
		serviceRef string
		isVOD      bool
		wantOrigin ports.SourceType
		wantFormat ports.SourceFormat
	}{
		{
			name:       "DVB tuner reference",
			serviceRef: "1:0:19:283D:3FB:1:C00000:0:0:0:",
			isVOD:      false,
			wantOrigin: ports.SourceTuner,
			wantFormat: ports.SourceFormatMPEGTS,
		},
		{
			name:       "Direct HTTP MPEG-TS URL",
			serviceRef: "http://10.10.55.100:8000/live/stream.ts",
			isVOD:      false,
			wantOrigin: ports.SourceIPTV,
			wantFormat: ports.SourceFormatMPEGTS,
		},
		{
			name:       "Direct HTTP HLS playlist URL",
			serviceRef: "http://10.10.55.100:8000/live/stream.m3u8",
			isVOD:      false,
			wantOrigin: ports.SourceIPTV,
			wantFormat: ports.SourceFormatHLS,
		},
		{
			name:       "Direct HTTPS HLS URL with format parameter",
			serviceRef: "https://example.com/iptv/live?format=hls&feed=main",
			isVOD:      false,
			wantOrigin: ports.SourceIPTV,
			wantFormat: ports.SourceFormatHLS,
		},
		{
			name:       "Opaque IPTV ID prefix",
			serviceRef: "iptv_abcdef123456",
			isVOD:      false,
			wantOrigin: ports.SourceIPTV,
			wantFormat: ports.SourceFormatMPEGTS,
		},
		{
			name:       "VOD recording",
			serviceRef: "/media/hdd/movie/recording.ts",
			isVOD:      true,
			wantOrigin: ports.SourceFile,
			wantFormat: ports.SourceFormatFile,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOrigin, gotFormat := classifySessionSource(tt.serviceRef, tt.isVOD)
			assert.Equal(t, tt.wantOrigin, gotOrigin, "SourceType origin mismatch")
			assert.Equal(t, tt.wantFormat, gotFormat, "SourceFormat format mismatch")
		})
	}
}

// TestCopyMode_PackagerTimeout_PromotesToRepair verifies that when copy mode times out
// packaging segments, it recovers by promoting to ProfileRepair (transcoding).
func TestCopyMode_PackagerTimeout_PromotesToRepair(t *testing.T) {
	copyProfile := model.ProfileSpec{
		Name:           profiles.ProfileHigh,
		TranscodeVideo: false,
		DVRWindowSec:   1800,
	}

	next, promote := startupRecoveryProfileWithResolver(
		copyProfile,
		model.RPackagerFailed,
		"playlist not ready timeout (last reason: not enough segments: 2 < 4 required)",
		nil,
	)

	require.True(t, promote, "copy profile on packager timeout must promote to transcode repair")
	assert.Equal(t, profiles.ProfileRepair, next.Name)
	assert.Equal(t, 1800, next.DVRWindowSec)
	assert.Equal(t, ports.RuntimeModeSourceRuntimeHardening, next.EffectiveModeSource)
}

// TestStartupBudget_CopyVsTranscodeDifferentiatedBudgeting verifies that copy mode
// uses coarse 10s floor while retry reserves transcode floor (6s).
func TestStartupBudget_CopyVsTranscodeDifferentiatedBudgeting(t *testing.T) {
	orch := &Orchestrator{
		LiveStartupBudget:  45 * time.Second,
		LiveReadySegments:  3,
		LiveSegmentSeconds: 2,
	}

	// Copy profile
	copySpec := model.ProfileSpec{
		Name:           profiles.ProfileHigh,
		TranscodeVideo: false,
	}

	start := time.Now()
	budget := orch.newStartupBudget(start, false, copySpec)

	assert.Equal(t, 10*time.Second, budget.ReadyFloor, "copy mode must floor ready media at 10s")
	assert.Equal(t, 6*time.Second, budget.RetryFloor, "retry floor must reflect transcode geometry 6s")

	assert.True(t, budget.fitsRetry(), "45s budget must fit copy attempt (22s) and transcode retry (18s)")

	attempt0 := budget.attempt(0, false)
	assert.Equal(t, 18*time.Second, attempt0.Reserve, "attempt 0 must reserve retryCost (18s), not copy cost")

	usable, bounded := attempt0.usable(start)
	assert.True(t, bounded)
	assert.Equal(t, 27*time.Second, usable, "attempt 0 usable must be 45s - 18s = 27s")

	// Recovery attempt (attempt 1)
	attempt1 := budget.attempt(1, true)
	assert.Equal(t, time.Duration(0), attempt1.Reserve, "last attempt has 0 reserve")
	assert.Equal(t, 6*time.Second, attempt1.ReadyFloor, "recovery attempt uses transcode ready floor (6s)")
}
