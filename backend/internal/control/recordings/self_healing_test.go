package recordings_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ManuGH/xg2g/internal/control/recordings"
	"github.com/ManuGH/xg2g/internal/control/vod"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile"
	"github.com/ManuGH/xg2g/internal/domain/playbackprofile/ports"
)

type mockSelfHealRunner struct {
	startedCount int
	lastSpec     vod.Spec
}

func (r *mockSelfHealRunner) Start(ctx context.Context, spec vod.Spec) (vod.Handle, error) {
	r.startedCount++
	r.lastSpec = spec
	fmt.Printf("MOCK_RUNNER_START called! count=%d\n", r.startedCount)
	if spec.WorkDir != "" && spec.OutputTemp != "" {
		_ = os.WriteFile(filepath.Join(spec.WorkDir, spec.OutputTemp), []byte("#EXTM3U\n#EXT-X-VERSION:3\n"), 0644)
	}
	return &mockSelfHealHandle{}, nil
}

type mockSelfHealHandle struct{}

func (h *mockSelfHealHandle) Wait() error                          { return nil }
func (h *mockSelfHealHandle) Stop(grace, kill time.Duration) error { return nil }
func (h *mockSelfHealHandle) Progress() <-chan vod.ProgressEvent   { return nil }
func (h *mockSelfHealHandle) Diagnostics() []string                { return nil }

type mockSelfHealProber struct {
	info *vod.StreamInfo
	err  error
}

func (p *mockSelfHealProber) Probe(ctx context.Context, path string) (*vod.StreamInfo, error) {
	fmt.Printf("MOCK_PROBER_PROBE called for path=%s info=%+v\n", path, p.info)
	return p.info, p.err
}

// TestPhase4_SelfHealing_Integration directly proves the Phase 4 self-healing loop:
// 1. Seed stale truth (SourceTruth claims h264/mp4, but real file is mpeg2video/mpegts).
// 2. First build attempt fails with JobStateFailed and ReasonTruthMismatch.
// 3. LoadRecordingBuildState detects TruthMismatch and invalidates cached truth via InvalidateTruth.
// 4. Simulated Re-Poll generates a fresh BuildIntent and successfully builds.
// 5. Assertion: Exactly one failed job and one successful job across the self-healing cycle.
func TestPhase4_SelfHealing_Integration(t *testing.T) {
	ctx := context.Background()
	hlsRoot := t.TempDir()
	serviceRef := "1:0:0:0:0:0:0:0:0:0:/movie/self-healing.ts"
	variant := "default"

	cacheDir, err := recordings.RecordingVariantCacheDir(hlsRoot, serviceRef, variant)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(cacheDir, 0755))

	metaID := recordings.RecordingVariantMetadataKey(serviceRef, variant)
	finalPath := filepath.Join(cacheDir, "index.m3u8")

	// Real prober finds mpeg2video / mpegts
	realInfo := &vod.StreamInfo{
		Container: "mpegts",
		Video: vod.VideoStreamInfo{
			CodecName: "mpeg2video",
			Height:    1080,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "mp2",
		},
	}
	runner := &mockSelfHealRunner{}
	prober := &mockSelfHealProber{info: realInfo}
	vodManager, err := vod.NewManager(runner, prober, nil)
	require.NoError(t, err)

	// Step 1: Seed stale truth in metadata claiming the recording is mp4 / h264
	staleInfo := &vod.StreamInfo{
		Container: "mp4",
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
			Height:    1080,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "aac",
		},
	}
	vodManager.MarkProbed(metaID, finalPath, staleInfo, nil)
	seededMeta, ok := vodManager.GetMetadata(metaID)
	require.True(t, ok)
	assert.Equal(t, "mp4", seededMeta.Container)
	assert.Equal(t, "h264", seededMeta.VideoCodec)

	// Step 2: First build attempt using stale SourceTruth
	intent1 := &ports.BuildIntent{
		SourceTruth: ports.SourceProfile{
			Container:  "mp4",
			VideoCodec: "h264",
		},
		Target: playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Video:     playbackprofile.VideoTarget{Codec: "h264"},
		},
	}
	job1, err := vodManager.EnsureSpec(ctx, cacheDir, metaID, finalPath, cacheDir, "index.live.m3u8", finalPath, intent1)
	require.NoError(t, err)
	require.NotNil(t, job1)

	// Wait for BuildMonitor to probe, detect mismatch, and fail the job
	assert.Eventually(t, func() bool {
		meta, exists := vodManager.GetMetadata(metaID)
		return exists && meta.State == vod.ArtifactStateFailed && meta.Error == string(vod.ReasonTruthMismatch)
	}, 2*time.Second, 20*time.Millisecond, "Expected first build to fail with TruthMismatch")

	failedMeta, _ := vodManager.GetMetadata(metaID)
	assert.Equal(t, vod.ArtifactStateFailed, failedMeta.State)
	assert.Equal(t, string(vod.ReasonTruthMismatch), failedMeta.Error)

	// Step 3: Recordings layer polls build state; should detect TruthMismatch and invalidate truth
	_, _, metaAfterFail, metaOk, err := recordings.LoadRecordingBuildState(ctx, hlsRoot, vodManager, serviceRef, variant)
	require.NoError(t, err)
	require.True(t, metaOk)
	assert.Equal(t, vod.ArtifactStateFailed, metaAfterFail.State)

	// Verify that InvalidateTruth cleared the cached container and codecs
	assert.Empty(t, metaAfterFail.Container, "Container truth must be cleared after TruthMismatch")
	assert.Empty(t, metaAfterFail.VideoCodec, "VideoCodec truth must be cleared after TruthMismatch")
	assert.Empty(t, metaAfterFail.AudioCodec, "AudioCodec truth must be cleared after TruthMismatch")

	// Step 4: Simulated re-poll generates fresh BuildIntent aligned with the actual probed media
	intent2 := &ports.BuildIntent{
		SourceTruth: ports.SourceProfile{
			Container:  realInfo.Container,
			VideoCodec: realInfo.Video.CodecName,
			AudioCodec: realInfo.Audio.CodecName,
		},
		Target: playbackprofile.TargetPlaybackProfile{
			Container: "mpegts",
			Video:     playbackprofile.VideoTarget{Codec: "h264"},
		},
	}

	job2, err := vodManager.EnsureSpec(ctx, cacheDir, metaID, finalPath, cacheDir, "index.live.m3u8", finalPath, intent2)
	require.NoError(t, err)
	require.NotNil(t, job2)

	// Wait for second build to succeed
	assert.Eventually(t, func() bool {
		meta, exists := vodManager.GetMetadata(metaID)
		return exists && meta.State == vod.ArtifactStateReady
	}, 2*time.Second, 20*time.Millisecond, "Expected second build to succeed after self-healing")

	succeededMeta, _ := vodManager.GetMetadata(metaID)
	assert.Equal(t, vod.ArtifactStateReady, succeededMeta.State)

	// Step 5: Final Assertion: exactly one failed job (TruthMismatch) and one successful job across the cycle
	assert.Equal(t, 1, runner.startedCount, "Runner should only be started once (for the valid intent after self-healing)")
	assert.Equal(t, "mpeg2video", runner.lastSpec.Intent.SourceTruth.VideoCodec)
}
