package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	recordingcaps "github.com/ManuGH/xg2g/internal/control/recordings/capabilities"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSelectStorageDecisionSweepServices_RequiresExplicitScope(t *testing.T) {
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(testPremiumPlaylist()), 0600))

	_, err := selectStorageDecisionSweepServices(playlistPath, storageDecisionSweepOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires --bouquet, --channel, or --service-ref")
}

func TestSelectStorageDecisionSweepServices_PrefersKnownChannelsAndRespectsLimit(t *testing.T) {
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u8")
	require.NoError(t, os.WriteFile(playlistPath, []byte(`#EXTM3U
#EXTINF:-1 group-title="Premium",Zeta News
http://127.0.0.1/web/stream.m3u?ref=1:0:1:ZZZ:1:1:1:0:0:0:&name=Zeta%20News
#EXTINF:-1 group-title="Premium",ATV HD
http://127.0.0.1/web/stream.m3u?ref=1:0:1:ATV:1:1:1:0:0:0:&name=ATV%20HD
#EXTINF:-1 group-title="Premium",13th Street
http://127.0.0.1/web/stream.m3u?ref=1:0:1:13T:1:1:1:0:0:0:&name=13th%20Street
#EXTINF:-1 group-title="Premium",Alpha Extra
http://127.0.0.1/web/stream.m3u?ref=1:0:1:ALP:1:1:1:0:0:0:&name=Alpha%20Extra
#EXTINF:-1 group-title="Premium",DAZN 1
http://127.0.0.1/web/stream.m3u?ref=1:0:1:DAZ:1:1:1:0:0:0:&name=DAZN%201
`), 0600))

	selections, err := selectStorageDecisionSweepServices(playlistPath, storageDecisionSweepOptions{
		Bouquet: "Premium",
		Limit:   3,
	})
	require.NoError(t, err)
	require.Len(t, selections, 3)
	assert.Equal(t, "13th Street", selections[0].Name)
	assert.Equal(t, "DAZN 1", selections[1].Name)
	assert.Equal(t, "ATV HD", selections[2].Name)
}

func TestRunStorageDecisionSweep_UsesExecutorAndWritesJSON(t *testing.T) {
	oldExec := storageDecisionSweepExecutor
	t.Cleanup(func() { storageDecisionSweepExecutor = oldExec })

	outPath := filepath.Join(t.TempDir(), "sweep.json")
	var gotOpts storageDecisionSweepOptions
	storageDecisionSweepExecutor = func(opts storageDecisionSweepOptions) (storageDecisionSweep, error) {
		gotOpts = opts
		return storageDecisionSweep{
			GeneratedAt:      time.Unix(123, 0).UTC(),
			DataDir:          opts.DataDir,
			Playlist:         opts.PlaylistName,
			Bouquet:          opts.Bouquet,
			RequestedProfile: opts.RequestedProfile,
			ClientFamilies:   splitCSVString(opts.ClientFamiliesCSV),
			Summary: storageDecisionSweepSummary{
				ServicesSelected:     1,
				TruthComplete:        1,
				ServicesWithDecision: 1,
				DecisionRows:         2,
			},
		}, nil
	}

	code := runStorageDecisionSweep([]string{
		"--config", "/tmp/config.yaml",
		"--data-dir", "/tmp/data",
		"--playlist", "playlist.m3u8",
		"--bouquet", "Premium",
		"--client-family", "ios_safari_native,chromium_hlsjs",
		"--skip-scan",
		"--requested-profile", "quality",
		"--format", "json",
		"--out", outPath,
	})
	require.Equal(t, 0, code)
	assert.Equal(t, "/tmp/config.yaml", gotOpts.ConfigPath)
	assert.Equal(t, "/tmp/data", gotOpts.DataDir)
	assert.Equal(t, "Premium", gotOpts.Bouquet)
	assert.Equal(t, "ios_safari_native,chromium_hlsjs", gotOpts.ClientFamiliesCSV)
	assert.True(t, gotOpts.SkipScan)

	raw, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var decoded storageDecisionSweep
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, 1, decoded.Summary.ServicesSelected)
	assert.Equal(t, 2, decoded.Summary.DecisionRows)
}

func TestRunStorageDecisionSweep_ReturnsOneWhenRelevantDiffDetected(t *testing.T) {
	oldExec := storageDecisionSweepExecutor
	t.Cleanup(func() { storageDecisionSweepExecutor = oldExec })

	outPath := filepath.Join(t.TempDir(), "sweep.json")
	storageDecisionSweepExecutor = func(opts storageDecisionSweepOptions) (storageDecisionSweep, error) {
		return storageDecisionSweep{
			GeneratedAt: time.Unix(123, 0).UTC(),
			DataDir:     opts.DataDir,
			Playlist:    opts.PlaylistName,
			Diff: &storageDecisionSweepDiff{
				BaselineFound:   true,
				RelevantChanges: 1,
			},
		}, nil
	}

	code := runStorageDecisionSweep([]string{
		"--data-dir", "/tmp/data",
		"--bouquet", "Premium",
		"--format", "json",
		"--out", outPath,
	})
	require.Equal(t, 1, code)
}

func TestStorageDecisionSweepFamilyCaps_UsesSSOTFixtureFallback(t *testing.T) {
	caps := storageDecisionSweepFamilyCaps("ios_safari_native")

	assert.Equal(t, recordingcaps.ClientCapsSourceFamilyFallback, caps.ClientCapsSource)
	assert.False(t, caps.RuntimeProbeUsed)
	assert.Equal(t, []string{"mp4", "ts"}, caps.Containers)
	assert.Equal(t, []string{"h264", "hevc"}, caps.VideoCodecs)
	assert.Equal(t, []string{"aac", "ac3", "mp3"}, caps.AudioCodecs)
	assert.Equal(t, "ios_safari", caps.DeviceType)
	assert.Equal(t, []string{"native"}, caps.HLSEngines)
	assert.Equal(t, "native", caps.PreferredHLSEngine)
}

func TestSummarizeStorageDecisionSweep_CountsSuccessesAndErrors(t *testing.T) {
	summary := summarizeStorageDecisionSweep(storageDecisionSweep{
		ScannedServices: []storageDecisionSweepScanRow{
			{ServiceRef: "svc-1", TruthStatus: reportTruthComplete, TruthSource: reportTruthSourceScan},
			{ServiceRef: "svc-2", TruthStatus: reportTruthIncomplete, TruthSource: reportTruthSourceFallback},
			{ServiceRef: "svc-3", TruthStatus: reportTruthMissing, TruthSource: reportTruthSourceUnresolved},
		},
		Decisions: []storageDecisionSweepDecision{
			{ServiceRef: "svc-1", ClientFamily: "chromium_hlsjs", Mode: "direct_play"},
			{ServiceRef: "svc-1", ClientFamily: "ios_safari_native", Mode: "transcode"},
			{ServiceRef: "svc-2", ClientFamily: "ios_safari_native", Error: "fallback failed"},
		},
	})

	assert.Equal(t, 3, summary.ServicesSelected)
	assert.Equal(t, 1, summary.TruthComplete)
	assert.Equal(t, 1, summary.TruthIncomplete)
	assert.Equal(t, 1, summary.TruthMissing)
	assert.Equal(t, 0, summary.TruthEventInactive)
	assert.Equal(t, 1, summary.ServicesWithDecision)
	assert.Equal(t, 3, summary.DecisionRows)
	assert.Equal(t, 1, summary.DecisionErrors)
	assert.Equal(t, 1, summary.TruthSourceScan)
	assert.Equal(t, 1, summary.TruthSourceFallback)
	assert.Equal(t, 1, summary.TruthSourceUnresolved)
	assert.Equal(t, 0, summary.TruthSourceEventInactive)
}

func TestDiffStorageDecisionSweep_FirstRunHasNoBaseline(t *testing.T) {
	current := storageDecisionSweep{
		GeneratedAt: time.Unix(200, 0).UTC(),
		ScopeKey:    "scope-a",
	}

	diff := diffStorageDecisionSweep(nil, current, "/tmp/last_sweep.json")
	require.NotNil(t, diff)
	assert.Equal(t, "/tmp/last_sweep.json", diff.StatePath)
	assert.False(t, diff.BaselineFound)
	assert.False(t, diff.ScopeChanged)
	assert.Zero(t, diff.RelevantChanges)
}

func TestStorageDecisionSweepHasRelevantDiff_IgnoresScopeReset(t *testing.T) {
	assert.False(t, storageDecisionSweepHasRelevantDiff(storageDecisionSweep{
		Diff: &storageDecisionSweepDiff{
			BaselineFound:   true,
			ScopeChanged:    true,
			RelevantChanges: 99,
		},
	}))
	assert.True(t, storageDecisionSweepHasRelevantDiff(storageDecisionSweep{
		Diff: &storageDecisionSweepDiff{
			BaselineFound:   true,
			RelevantChanges: 1,
		},
	}))
}

func TestDiffStorageDecisionSweep_DetectsModeTruthAndCoverageChanges(t *testing.T) {
	previous := storageDecisionSweep{
		GeneratedAt: time.Unix(100, 0).UTC(),
		ScopeKey:    "scope-a",
		ScannedServices: []storageDecisionSweepScanRow{
			{ServiceRef: "svc-alpha", ChannelName: "Alpha HD", TruthStatus: reportTruthComplete, Container: "ts", VideoCodec: "h264", AudioCodec: "aac"},
			{ServiceRef: "svc-gamma", ChannelName: "Gamma News", TruthStatus: reportTruthIncomplete, VideoCodec: "h264"},
		},
		Decisions: []storageDecisionSweepDecision{
			{ServiceRef: "svc-alpha", ChannelName: "Alpha HD", ClientFamily: "chromium_hlsjs", ModeCode: "direct_play", Mode: "direct_play"},
		},
	}
	finalizeStorageDecisionSweepCoverage(&previous)
	previous.Summary = summarizeStorageDecisionSweep(previous)

	current := storageDecisionSweep{
		GeneratedAt: time.Unix(200, 0).UTC(),
		ScopeKey:    "scope-a",
		ScannedServices: []storageDecisionSweepScanRow{
			{ServiceRef: "svc-alpha", ChannelName: "Alpha HD", TruthStatus: reportTruthComplete, Container: "ts", VideoCodec: "h264", AudioCodec: "ac3"},
			{ServiceRef: "svc-gamma", ChannelName: "Gamma News", TruthStatus: reportTruthIncomplete, VideoCodec: "h264"},
		},
		Decisions: []storageDecisionSweepDecision{
			{ServiceRef: "svc-alpha", ChannelName: "Alpha HD", ClientFamily: "chromium_hlsjs", ModeCode: "transcode", Mode: "transcode"},
			{ServiceRef: "svc-gamma", ChannelName: "Gamma News", ClientFamily: "ios_safari_native", ModeCode: "transcode", Mode: "transcode"},
		},
	}
	finalizeStorageDecisionSweepCoverage(&current)
	current.Summary = summarizeStorageDecisionSweep(current)

	diff := diffStorageDecisionSweep(&previous, current, "/tmp/last_sweep.json")
	require.NotNil(t, diff)
	require.True(t, diff.BaselineFound)
	require.NotNil(t, diff.BaselineGeneratedAt)
	assert.False(t, diff.ScopeChanged)
	assert.Equal(t, 3, diff.RelevantChanges)
	require.Len(t, diff.ModeChanges, 1)
	assert.Equal(t, "Alpha HD", diff.ModeChanges[0].ChannelName)
	assert.Equal(t, "direct_play", diff.ModeChanges[0].FromMode)
	assert.Equal(t, "transcode", diff.ModeChanges[0].ToMode)
	require.Len(t, diff.TruthChanges, 1)
	assert.Equal(t, "Alpha HD", diff.TruthChanges[0].ChannelName)
	assert.Equal(t, "ts/h264/aac", diff.TruthChanges[0].FromTruth)
	assert.Equal(t, "ts/h264/ac3", diff.TruthChanges[0].ToTruth)
	require.NotNil(t, diff.Coverage)
	assert.True(t, diff.Coverage.Regression)
	assert.Equal(t, 0, diff.Coverage.FallbackBefore)
	assert.Equal(t, 1, diff.Coverage.FallbackAfter)
	require.Len(t, diff.Coverage.NewFallback, 1)
	assert.Equal(t, "Gamma News", diff.Coverage.NewFallback[0].ChannelName)
}

func TestPersistAndLoadStorageDecisionSweepState_RoundTrip(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "store", "last_sweep.json")
	result := storageDecisionSweep{
		GeneratedAt:      time.Unix(300, 0).UTC(),
		DataDir:          "/tmp/data",
		Playlist:         "playlist.m3u8",
		Bouquet:          "Premium",
		RequestedProfile: "quality",
		SkipScan:         true,
		ScopeKey:         "scope-a",
		ClientFamilies:   []string{"chromium_hlsjs"},
		Summary: storageDecisionSweepSummary{
			ServicesSelected:     1,
			TruthComplete:        1,
			TruthSourceScan:      1,
			ServicesWithDecision: 1,
			DecisionRows:         1,
		},
		ScannedServices: []storageDecisionSweepScanRow{
			{ServiceRef: "svc-alpha", ChannelName: "Alpha HD", TruthStatus: reportTruthComplete, TruthSource: reportTruthSourceScan, Container: "ts", VideoCodec: "h264", AudioCodec: "aac"},
		},
		Diff: &storageDecisionSweepDiff{RelevantChanges: 99},
	}

	require.NoError(t, persistStorageDecisionSweepState(statePath, result))
	loaded, err := loadStorageDecisionSweepState(statePath)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "scope-a", loaded.ScopeKey)
	assert.Nil(t, loaded.Diff)
	assert.Len(t, loaded.ScannedServices, 1)
	assert.Equal(t, "Alpha HD", loaded.ScannedServices[0].ChannelName)
}
