package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/stretchr/testify/require"
)

func TestManager_ProbeCapability_StoresSuccessfulTargetedProbe(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video: vod.VideoStreamInfo{
				CodecName: "h264",
				Width:     1920,
				Height:    1080,
			},
			Audio: vod.AudioStreamInfo{
				CodecName: "aac",
			},
		}, nil
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, capability.HasMediaTruth())
	require.Equal(t, CapabilityStateOK, capability.State)

	stored, storedFound := store.Get(serviceRef)
	require.True(t, storedFound)
	require.Equal(t, "ts", stored.Container)
	require.Equal(t, "h264", stored.VideoCodec)
	require.Equal(t, "aac", stored.AudioCodec)
}

func TestManager_ProbeCapability_StoresFailedTargetedProbeState(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		return nil, errors.New("ffprobe failed: exit status 1")
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.Error(t, err)
	require.True(t, found)
	require.Equal(t, CapabilityStateFailed, capability.State)
	require.Equal(t, "ffprobe failed: exit status 1", capability.FailureReason)

	stored, storedFound := store.Get(serviceRef)
	require.True(t, storedFound)
	require.Equal(t, CapabilityStateFailed, stored.State)

	_, usable := manager.GetCapability(serviceRef)
	require.False(t, usable)
}
