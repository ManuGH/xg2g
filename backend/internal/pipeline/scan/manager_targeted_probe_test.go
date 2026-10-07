package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
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

func TestManager_ProbeCapability_DoesNotWaitForPlaybackIdle(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	var activePlaybackChecks atomic.Int32
	manager.ActivePlaybackFn = func(ctx context.Context) (bool, error) {
		activePlaybackChecks.Add(1)
		return true, nil
	}
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: "ts",
			Video:     vod.VideoStreamInfo{CodecName: "h264"},
			Audio:     vod.AudioStreamInfo{CodecName: "aac"},
		}, nil
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, capability.HasMediaTruth())
	require.Zero(t, activePlaybackChecks.Load())
}

func TestManager_ProbeCapability_IPTVDirectURL(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "4097:0:1:0:0:0:0:0:0:0:http%3A%2F%2Fsvods.net%3A8080%2Flive%2Fjewell.rosenbaum%2F1a05a123%2F311271.ts:XXX%3A%20Bangbros%204K"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	// Playlist has the channel with TvgID and xg2g stream URL
	content := "#EXTM3U\n#EXTINF:-1 tvg-id=\"" + serviceRef + "\",XXX: Bangbros 4K\nhttp://10.10.55.14:8089/api/v3/stream/live/iptv_r3whmrdrot5egz4gumioylec2a\n"
	require.NoError(t, os.WriteFile(playlistPath, []byte(content), 0o600))

	manager := NewManager(store, playlistPath, nil)
	var probedURL string
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		probedURL = probeURL
		return &vod.StreamInfo{
			Container: "mpegts",
			Video: vod.VideoStreamInfo{
				CodecName: "hevc",
				Width:     3840,
				Height:    2160,
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
	require.Equal(t, "3840x2160", capability.Resolution)
	require.Equal(t, "hevc", capability.VideoCodec)
	require.Equal(t, "http://svods.net:8080/live/jewell.rosenbaum/1a05a123/311271.ts", probedURL, "Probe URL must be clean unescaped upstream URL preserving case")

	stored, storedFound := store.Get(serviceRef)
	require.True(t, storedFound)
	require.Equal(t, "hevc", stored.VideoCodec)
	require.Equal(t, "3840x2160", stored.Resolution)
}

func TestManager_LookupChannel_IPTVFallbackWhenNotInPlaylist(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "4097:0:1:0:0:0:0:0:0:0:http%3a//svods.net%3a8080/live/user/pass/999.ts:Channel Name"
	emptyPlaylist := filepath.Join(t.TempDir(), "empty.m3u")
	require.NoError(t, os.WriteFile(emptyPlaylist, []byte("#EXTM3U\n"), 0o600))

	manager := NewManager(store, emptyPlaylist, nil)
	ch, found, err := manager.lookupChannel(serviceRef)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "http://svods.net:8080/live/user/pass/999.ts", ch.URL)
	require.Equal(t, "Channel Name", ch.Name)
}
