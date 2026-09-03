// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

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

type stubLiveProbeSource struct {
	path         string
	err          error
	live         bool
	calls        int
	releases     int
	requestedMin []int
}

func (s *stubLiveProbeSource) SnapshotHead(_ context.Context, _ string, minBytes int) (string, func(), error) {
	s.calls++
	s.requestedMin = append(s.requestedMin, minBytes)
	if s.err != nil {
		return "", nil, s.err
	}
	return s.path, func() { s.releases++ }, nil
}

func (s *stubLiveProbeSource) IsLive(string) bool { return s.live }

func newProbeManager(t *testing.T, serviceRef string) (*Manager, *MemoryStore) {
	t.Helper()
	store := NewMemoryStore()
	playlistPath := filepath.Join(t.TempDir(), "playlist.m3u")
	require.NoError(t, os.WriteFile(playlistPath, []byte("#EXTM3U\n#EXTINF:-1,Test\nhttp://receiver.example/"+serviceRef+"\n"), 0o600))
	return NewManager(store, playlistPath, nil), store
}

// A probe that dials the receiver itself puts a second connection on a service the
// shared ingest may already be streaming, and when that second connection closes
// the receiver rebuilds the program's CA PMT and stops descrambling for the
// session still playing it. So with an ingest to read from, the probe reads the
// ingest - and no URL reaches ffprobe at all.
func TestProbeCapability_ReadsTheSharedIngestInsteadOfTheReceiver(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	manager, store := newProbeManager(t, serviceRef)

	snapshot := filepath.Join(t.TempDir(), "head.ts")
	require.NoError(t, os.WriteFile(snapshot, []byte("head"), 0o600))
	src := &stubLiveProbeSource{path: snapshot, live: true}
	manager.SetLiveProbeSource(src)

	var probed []string
	manager.probeFn = func(_ context.Context, probeURL string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		probed = append(probed, probeURL)
		return &vod.StreamInfo{
			Container:   "ts",
			Video:       vod.VideoStreamInfo{CodecName: "h264", Width: 1920, Height: 1080},
			Audio:       vod.AudioStreamInfo{CodecName: "ac3"},
			BitrateKbps: 8000,
		}, nil
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, capability.HasMediaTruth())

	require.Equal(t, []string{snapshot}, probed, "ffprobe must be pointed at the ingest snapshot, never at a receiver URL")
	require.Equal(t, 1, src.calls)
	require.Equal(t, 1, src.releases, "the snapshot must be dropped once the probe is done with it")

	stored, ok := store.Get(serviceRef)
	require.True(t, ok)
	require.Equal(t, "h264", stored.VideoCodec)
}

// When the ingest cannot deliver, the answer is a failed probe. Falling back to a
// receiver URL would reintroduce the very connection this path exists to avoid,
// and it would do so exactly when the receiver is least able to take it.
func TestProbeCapability_DoesNotFallBackToTheReceiverWhenTheIngestFails(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	manager, store := newProbeManager(t, serviceRef)

	src := &stubLiveProbeSource{err: errors.New("tuner topology admission denied")}
	manager.SetLiveProbeSource(src)

	probeCalls := 0
	manager.probeFn = func(_ context.Context, _ string, _ infra.ProbeOptions) (*vod.StreamInfo, error) {
		probeCalls++
		return &vod.StreamInfo{Container: "ts"}, nil
	}

	_, _, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.ErrorIs(t, err, ErrIngestSnapshotUnavailable)
	require.Zero(t, probeCalls, "no probe may run against a receiver URL once the ingest is the source of truth")

	_, ok := store.Get(serviceRef)
	require.False(t, ok, "an ingest that handed over nothing must write nothing about the channel")
}

// The extended retry means "read more of the stream". Against a fixed snapshot it
// could only re-read the same bytes and repeat the first answer, so the bigger
// budget has to reach the source that produces the head.
func TestProbeCapability_ExtendedRetryAsksTheIngestForABiggerHead(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	manager, _ := newProbeManager(t, serviceRef)

	snapshot := filepath.Join(t.TempDir(), "head.ts")
	require.NoError(t, os.WriteFile(snapshot, []byte("head"), 0o600))
	src := &stubLiveProbeSource{path: snapshot, live: true}
	manager.SetLiveProbeSource(src)

	manager.probeFn = func(_ context.Context, _ string, opts infra.ProbeOptions) (*vod.StreamInfo, error) {
		if opts.ProbeSizeBytes == 0 {
			// Incomplete media truth: video only, which is what triggers the retry.
			return &vod.StreamInfo{Video: vod.VideoStreamInfo{CodecName: "h264"}}, nil
		}
		return &vod.StreamInfo{
			Container:   "ts",
			Video:       vod.VideoStreamInfo{CodecName: "h264"},
			Audio:       vod.AudioStreamInfo{CodecName: "ac3"},
			BitrateKbps: 8000,
		}, nil
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "ac3", capability.AudioCodec, "the retry's richer truth must win")

	require.Equal(t, 2, src.calls)
	require.Equal(t, []int{0, extendedProbeSizeBytes}, src.requestedMin)
	require.Equal(t, 2, src.releases)
}

// A tuner that was busy is a fact about this minute, not about the channel. The
// old URL probe could not tell the two apart because it always ran; the ingest
// path can, and must, or one busy moment locks a channel out of scanning for a
// day.
func TestProbeCapability_IngestOutageDoesNotLockTheChannelOut(t *testing.T) {
	serviceRef := "1:0:1:ABC"
	manager, store := newProbeManager(t, serviceRef)

	good := Capability{
		ServiceRef: serviceRef,
		State:      CapabilityStateOK,
		Container:  "ts",
		VideoCodec: "h264",
		AudioCodec: "ac3",
	}
	store.Update(good)

	manager.SetLiveProbeSource(&stubLiveProbeSource{err: errors.New("tuner topology admission denied")})
	manager.probeFn = func(context.Context, string, infra.ProbeOptions) (*vod.StreamInfo, error) {
		t.Fatal("no probe may run when the ingest handed over nothing")
		return nil, nil
	}

	capability, found, err := manager.ProbeCapability(context.Background(), serviceRef)
	require.ErrorIs(t, err, ErrIngestSnapshotUnavailable)
	require.True(t, found)
	require.Equal(t, CapabilityStateOK, capability.State, "the outage must leave the known truth standing")

	stored, ok := store.Get(serviceRef)
	require.True(t, ok)
	require.Equal(t, CapabilityStateOK, stored.State)
	require.Equal(t, "h264", stored.VideoCodec)
}
