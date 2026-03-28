package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	infra "github.com/ManuGH/xg2g/internal/infra/ffmpeg"
	"github.com/stretchr/testify/require"
)

func TestIsRicherMediaTruth_StrictlyAdditive(t *testing.T) {
	t.Parallel()

	base := &vod.StreamInfo{
		Container: "ts",
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
			Width:     1920,
			Height:    1080,
		},
	}

	require.True(t, isRicherMediaTruth(base, &vod.StreamInfo{
		Container: "ts",
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
			Width:     1920,
			Height:    1080,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "ac3",
		},
	}))

	require.False(t, isRicherMediaTruth(base, &vod.StreamInfo{
		Container: "ts",
		Video: vod.VideoStreamInfo{
			CodecName: "hevc",
			Width:     1920,
			Height:    1080,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "ac3",
		},
	}))

	require.False(t, isRicherMediaTruth(base, &vod.StreamInfo{
		Container: "",
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
			Width:     1920,
			Height:    1080,
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "ac3",
		},
	}))
}

func TestManager_RunScan_ExtendedProbeRetryEnrichesMediaTruth(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	manager.ProbeDelay = 0

	var optsSeen []infra.ProbeOptions
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		optsSeen = append(optsSeen, opts)
		switch len(optsSeen) {
		case 1:
			return &vod.StreamInfo{
				Container: "ts",
				Video: vod.VideoStreamInfo{
					CodecName: "h264",
					Width:     1920,
					Height:    1080,
				},
			}, nil
		case 2:
			return &vod.StreamInfo{
				Container: "ts",
				Video: vod.VideoStreamInfo{
					CodecName: "h264",
					Width:     1920,
					Height:    1080,
				},
				Audio: vod.AudioStreamInfo{
					CodecName: "ac3",
				},
			}, nil
		default:
			t.Fatalf("unexpected extra probe call %d", len(optsSeen))
			return nil, nil
		}
	}

	require.NoError(t, manager.RunScan(context.Background()))

	cap, found := store.Get(serviceRef)
	require.True(t, found)
	require.True(t, cap.HasMediaTruth())
	require.Equal(t, "ts", cap.Container)
	require.Equal(t, "h264", cap.VideoCodec)
	require.Equal(t, "ac3", cap.AudioCodec)
	require.Len(t, optsSeen, 2)
	require.Zero(t, optsSeen[0].AnalyzeDuration)
	require.Zero(t, optsSeen[0].ProbeSizeBytes)
	require.Equal(t, extendedProbeAnalyzeDuration, optsSeen[1].AnalyzeDuration)
	require.EqualValues(t, extendedProbeSizeBytes, optsSeen[1].ProbeSizeBytes)
}

func TestManager_RunScan_ExtendedProbeRetryRecoversFromExistingPartial(t *testing.T) {
	store := NewMemoryStore()
	serviceRef := "1:0:1:ABC"
	now := time.Now().UTC()
	store.Update(Capability{
		ServiceRef:  serviceRef,
		Resolution:  "1920x1080",
		Codec:       "h264",
		VideoCodec:  "h264",
		State:       CapabilityStateOK,
		LastScan:    now.Add(-time.Hour),
		LastAttempt: now.Add(-time.Hour),
		LastSuccess: now.Add(-time.Hour),
		NextRetryAt: now.Add(-time.Minute),
	})

	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example:8001/"+serviceRef+"\n"), 0o600))

	manager := NewManager(store, playlistPath, nil)
	manager.ProbeDelay = 0

	var optsSeen []infra.ProbeOptions
	manager.probeFn = func(ctx context.Context, probeURL string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		optsSeen = append(optsSeen, opts)
		switch len(optsSeen) {
		case 1:
			return nil, errors.New("ffprobe failed: exit status 1")
		case 2:
			return &vod.StreamInfo{
				Container: "ts",
				Video: vod.VideoStreamInfo{
					CodecName: "h264",
					Width:     1920,
					Height:    1080,
				},
				Audio: vod.AudioStreamInfo{
					CodecName: "ac3",
				},
			}, nil
		default:
			t.Fatalf("unexpected extra probe call %d", len(optsSeen))
			return nil, nil
		}
	}

	require.NoError(t, manager.RunScan(context.Background()))

	cap, found := store.Get(serviceRef)
	require.True(t, found)
	require.True(t, cap.HasMediaTruth())
	require.Equal(t, "ac3", cap.AudioCodec)
	require.Len(t, optsSeen, 2)
	require.Zero(t, optsSeen[0].AnalyzeDuration)
	require.Equal(t, extendedProbeAnalyzeDuration, optsSeen[1].AnalyzeDuration)
	require.EqualValues(t, extendedProbeSizeBytes, optsSeen[1].ProbeSizeBytes)
}
