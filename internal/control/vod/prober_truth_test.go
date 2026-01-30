package vod

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mocks

type MockInfraProber struct {
	mock.Mock
}

func (m *MockInfraProber) Probe(ctx context.Context, path string) (*StreamInfo, error) {
	args := m.Called(ctx, path)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*StreamInfo), args.Error(1)
}

type MockMapper struct{}

func (m *MockMapper) ResolveLocalExisting(p string) (string, bool) { return p, true }

func setupManager(t *testing.T) (*Manager, *MockInfraProber, string) {
	prober := new(MockInfraProber)
	mgr, err := NewManager(&mockRunner{}, prober, &MockMapper{})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	f, err := os.CreateTemp("", "test*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	return mgr, prober, f.Name()
}

// Tests

func TestTruth_Write_B1_Success(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Mock successful probe > 0
	info := &StreamInfo{
		Video:     VideoStreamInfo{Duration: 60.5, CodecName: "h264"},
		Audio:     AudioStreamInfo{CodecName: "aac"},
		Container: "mov,mp4",
	}
	prober.On("Probe", mock.Anything, path).Return(info, nil)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	assert.NoError(t, err)

	// Verify Metadata Updated
	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStateReady, meta.State)
	assert.Equal(t, int64(61), meta.Duration)
}

func TestTruth_Write_B2_ZeroDuration_Guard(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Setup: Existing valid metadata
	mgr.SeedMetadata(ref, Metadata{
		State:     ArtifactStateReady,
		Duration:  3600,
		UpdatedAt: time.Now().Unix(),
	})

	// Mock probe returning 0 duration (but valid file?)
	info := &StreamInfo{
		Video: VideoStreamInfo{Duration: 0, CodecName: "h264"},
	}
	prober.On("Probe", mock.Anything, path).Return(info, nil)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	// Expectation: Error returned (Invalid Duration)
	assert.Error(t, err)

	// Verify Metadata NOT overwritten with 0
	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, int64(3600), meta.Duration)
}

func TestTruth_Write_B3_Timeout_Mapping(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	// Mock Timeout
	prober.On("Probe", mock.Anything, path).Return(nil, context.DeadlineExceeded)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	// Expectation: specific error, State should NOT be Ready
	assert.Error(t, err)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStatePreparing, meta.State)
}

func TestTruth_Write_B4_Corrupt(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref"

	prober.On("Probe", mock.Anything, path).Return(nil, errors.New("corrupt file"))

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	assert.Error(t, err)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStateFailed, meta.State)
}

// B5: Probe failure MUST preserve existing Duration (Option A invariant).
// This test proves:
// 1. Pre-condition: Metadata has valid Duration (3600s)
// 2. Action: Probe fails → MarkFailure is called
// 3. Post-condition: Duration unchanged, but State/Error DID change (proves MarkFailure ran)
func TestTruth_Write_B5_ProbeFailPreservesDuration(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref:b5"

	// Pre-condition: Existing metadata with valid duration
	originalDuration := int64(3600)
	mgr.SeedMetadata(ref, Metadata{
		State:     ArtifactStateReady,
		Duration:  originalDuration,
		UpdatedAt: time.Now().Add(-1 * time.Hour).Unix(), // Old timestamp
	})

	// Mock: Probe returns network error (not corrupt, not timeout)
	probeErr := errors.New("network unreachable")
	prober.On("Probe", mock.Anything, path).Return(nil, probeErr)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	// Must return error
	assert.Error(t, err)

	// Read back metadata via same path as API/Decision Engine
	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok, "metadata must exist after probe failure")

	// INVARIANT: Duration preserved (Option A)
	assert.Equal(t, originalDuration, meta.Duration, "Duration MUST NOT be degraded by probe failure")

	// Proof that MarkFailure executed (side-effects):
	assert.Equal(t, ArtifactStateFailed, meta.State, "State must change to Failed")
	assert.Contains(t, meta.Error, "probe_failed", "Error must be set by MarkFailure")
	assert.Greater(t, meta.UpdatedAt, int64(0), "UpdatedAt must be touched")
}

func TestTruth_Write_B6_MarkProbedClearsArtifactPathForNonMP4(t *testing.T) {
	mgr, _, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref:b6"

	mgr.SeedMetadata(ref, Metadata{
		ArtifactPath: "old.mp4",
		PlaylistPath: "old.m3u8",
	})

	info := &StreamInfo{
		Video: VideoStreamInfo{Duration: 10},
	}

	mgr.MarkProbed(ref, "/tmp/source.ts", info, &Fingerprint{})

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Empty(t, meta.ArtifactPath)
	assert.Empty(t, meta.PlaylistPath)
	assert.Equal(t, "/tmp/source.ts", meta.ResolvedPath)
}

func TestTruth_Write_B7_MarkProbedSetsPlaylistPathForM3U8(t *testing.T) {
	mgr, _, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref:b7"

	mgr.SeedMetadata(ref, Metadata{
		ArtifactPath: "old.mp4",
	})

	info := &StreamInfo{
		Video: VideoStreamInfo{Duration: 10},
	}

	mgr.MarkProbed(ref, "/tmp/playlist.m3u8", info, &Fingerprint{})

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Empty(t, meta.ArtifactPath)
	assert.Equal(t, "/tmp/playlist.m3u8", meta.PlaylistPath)
	assert.Equal(t, "/tmp/playlist.m3u8", meta.ResolvedPath)
}

func TestTruth_Write_B8_MarkProbedDoesNotDegradeVideoFields(t *testing.T) {
	mgr, _, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref:b8"

	mgr.SeedMetadata(ref, Metadata{
		Width:      1920,
		Height:     1080,
		FPS:        29.97,
		Interlaced: true,
	})

	info := &StreamInfo{
		Video: VideoStreamInfo{
			Duration:   10,
			Width:      0,
			Height:     0,
			FPS:        0,
			Interlaced: false,
		},
	}

	mgr.MarkProbed(ref, "", info, nil)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, 1920, meta.Width)
	assert.Equal(t, 1080, meta.Height)
	assert.Equal(t, 29.97, meta.FPS)
	assert.True(t, meta.Interlaced)
}

func TestTruth_Write_B9_CanceledIsFailed(t *testing.T) {
	mgr, prober, path := setupManager(t)
	defer os.Remove(path)
	ref := "test:ref:b9"

	prober.On("Probe", mock.Anything, path).Return(nil, context.Canceled)

	req := probeRequest{ServiceRef: ref, InputPath: path}
	err := mgr.runProbe(context.Background(), req)

	assert.Error(t, err)

	meta, ok := mgr.GetMetadata(ref)
	assert.True(t, ok)
	assert.Equal(t, ArtifactStateFailed, meta.State)
	assert.Equal(t, "probe_canceled", meta.Error)
}
