package vod

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_MarkProbed_PreservesFields(t *testing.T) {
	mgr := NewManager(&mockRunner{}, &mockProber{}, nil)
	id := "probing:ref"

	// 1. Seed initial metadata (e.g. from previous partial state or manual set)
	initialTime := time.Now().UnixNano()
	initialMeta := Metadata{
		State:        ArtifactStatePreparing, // Was preparing
		ResolvedPath: "/data/movie.ts",       // We knew the path
		PlaylistPath: "/cache/movie.m3u8",    // We had a playlist potentially
		ArtifactPath: "/cache/movie.mp4",     // Or an artifact
		StateGen:     5,
	}
	mgr.SeedMetadata(id, initialMeta)

	// Sleep to guarantee timestamp increment
	time.Sleep(1 * time.Microsecond)

	// 2. Mock Probe Result
	info := &StreamInfo{
		Container: "mp4",
		Video: VideoStreamInfo{
			CodecName: "h264",
			Duration:  120.5,
		},
		Audio: AudioStreamInfo{
			CodecName: "aac",
		},
	}

	// 3. Call MarkProbed
	mgr.MarkProbed(id, "", info, nil)

	// 4. Verify
	finalMeta, ok := mgr.GetMetadata(id)
	require.True(t, ok)

	// A. Updates from Probe
	assert.Equal(t, "mp4", finalMeta.Container)
	assert.Equal(t, "h264", finalMeta.VideoCodec)
	assert.Equal(t, "aac", finalMeta.AudioCodec)
	assert.Equal(t, int64(121), finalMeta.Duration)
	assert.Equal(t, ArtifactStateReady, finalMeta.State)
	assert.Equal(t, "", finalMeta.Error)

	// B. Preserved Fields
	assert.Equal(t, "/data/movie.ts", finalMeta.ResolvedPath)
	assert.Equal(t, "/cache/movie.m3u8", finalMeta.PlaylistPath)
	assert.Equal(t, "/cache/movie.mp4", finalMeta.ArtifactPath)

	// C. Timestamp & Gen
	assert.Greater(t, finalMeta.UpdatedAt, initialTime)
	assert.Equal(t, uint64(6), finalMeta.StateGen)
}

func TestManager_MarkFailure_PreservesFields(t *testing.T) {
	mgr := NewManager(&mockRunner{}, &mockProber{}, nil)
	id := "fail:ref"

	// 1. Seed initial meaningful metadata
	initialMeta := Metadata{
		State:        ArtifactStateUnknown,
		ResolvedPath: "/data/movie.ts",
		PlaylistPath: "/cache/movie.m3u8",
		ArtifactPath: "/cache/movie.mp4",
		Duration:     500,
		StateGen:     10,
	}
	mgr.SeedMetadata(id, initialMeta)

	// 2. Call MarkFailure (e.g. transient probe timeout)
	mgr.MarkFailure(id, ArtifactStatePreparing, "timeout", "", nil)

	// 3. Verify
	finalMeta, ok := mgr.GetMetadata(id)
	require.True(t, ok)

	// Changed
	assert.Equal(t, ArtifactStatePreparing, finalMeta.State)
	assert.Equal(t, "timeout", finalMeta.Error)
	assert.Greater(t, finalMeta.StateGen, uint64(10))

	// Preserved
	assert.Equal(t, "/data/movie.ts", finalMeta.ResolvedPath)
	assert.Equal(t, "/cache/movie.m3u8", finalMeta.PlaylistPath)
	assert.Equal(t, "/cache/movie.mp4", finalMeta.ArtifactPath)
	assert.Equal(t, int64(500), finalMeta.Duration)
}
