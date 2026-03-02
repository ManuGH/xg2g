package vod

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockRunner struct{}

func (r *mockRunner) Start(ctx context.Context, spec Spec) (Handle, error) { return &mockHandle{}, nil }

type mockHandle struct{}

func (h *mockHandle) Wait() error                          { return nil }
func (h *mockHandle) Stop(grace, kill time.Duration) error { return nil }
func (h *mockHandle) Progress() <-chan ProgressEvent       { return nil }
func (h *mockHandle) Diagnostics() []string                { return nil }

type mockProber struct{}

func (p *mockProber) Probe(ctx context.Context, path string) (*StreamInfo, error) { return nil, nil }

func TestManager_MarkFailed_PreservesFields(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)
	id := "service:ref"

	// 1. Seed initial metadata with rich state
	initialTime := time.Now().UnixNano()
	initialMeta := Metadata{
		State:        ArtifactStateReady,
		ResolvedPath: "/data/movie.ts",
		PlaylistPath: "/cache/movie.m3u8",
		Container:    "hls",
		VideoCodec:   "h264",
		AudioCodec:   "aac",
		UpdatedAt:    initialTime,
		StateGen:     1,
	}
	mgr.SeedMetadata(id, initialMeta)

	// Ensure clear time gap for monotonicity check (OS clock granularity)
	time.Sleep(1 * time.Microsecond)

	// 2. Call MarkFailed
	mgr.MarkFailed(id, "probe failed")

	// 3. Verify Result
	finalMeta, ok := mgr.GetMetadata(id)
	require.True(t, ok)

	// A. Check State Transition
	assert.Equal(t, ArtifactStateFailed, finalMeta.State)
	assert.Equal(t, "probe failed", finalMeta.Error)

	// B. Check Field Preservation (Non-destructive)
	assert.Equal(t, "/data/movie.ts", finalMeta.ResolvedPath)
	assert.Equal(t, "/cache/movie.m3u8", finalMeta.PlaylistPath)
	assert.Equal(t, "hls", finalMeta.Container)
	assert.Equal(t, "h264", finalMeta.VideoCodec)

	// C. Check Timestamp & Generation
	assert.Greater(t, finalMeta.UpdatedAt, initialTime, "UpdatedAt should increase monotonically")
	assert.Equal(t, uint64(2), finalMeta.StateGen, "StateGen should increment")
}

func TestManager_MarkFailed_InitializeUnknown(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)
	id := "new:ref"

	// Call MarkFailed on non-existent item
	mgr.MarkFailed(id, "startup fail")

	finalMeta, ok := mgr.GetMetadata(id)
	require.True(t, ok)
	assert.Equal(t, ArtifactStateFailed, finalMeta.State)
	assert.Equal(t, "startup fail", finalMeta.Error)
	assert.NotZero(t, finalMeta.UpdatedAt)
}

func TestManager_ActiveJobIDs_ReturnsSortedSnapshot(t *testing.T) {
	mgr, err := NewManager(&mockRunner{}, &mockProber{}, nil)
	require.NoError(t, err)

	mgr.mu.Lock()
	mgr.jobs["/tmp/recordings/c"] = nil
	mgr.jobs["/tmp/recordings/a"] = nil
	mgr.jobs["/tmp/recordings/b"] = nil
	mgr.mu.Unlock()

	ids := mgr.ActiveJobIDs()
	require.Equal(t, []string{
		"/tmp/recordings/a",
		"/tmp/recordings/b",
		"/tmp/recordings/c",
	}, ids)
}
