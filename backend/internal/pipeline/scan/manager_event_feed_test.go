package scan

import (
	"errors"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/stretchr/testify/assert"
)

func TestManager_MergeFailedAttempt_ClassifiesInactiveEventFeed(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.mergeFailedAttempt(Capability{}, false, "1:0:1:EVENT", "Sky Sport Austria 3", now, errors.New("ffprobe failed: signal: killed (stderr: )"))

	assert.Equal(t, CapabilityStateInactiveEventFeed, cap.State)
	assert.Equal(t, "ffprobe failed: signal: killed (stderr: )", cap.FailureReason)
	assert.True(t, cap.NextRetryAt.Equal(now.Add(failureRetryWindow)))
}

func TestManager_MergeFailedAttempt_LeavesNonEventSourceFailed(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.mergeFailedAttempt(Capability{}, false, "1:0:1:LINEAR", "EUROSPORT 2", now, errors.New("ffprobe failed: signal: killed (stderr: )"))

	assert.Equal(t, CapabilityStateFailed, cap.State)
	assert.Equal(t, "ffprobe failed: signal: killed (stderr: )", cap.FailureReason)
}

func TestManager_CapabilityFromProbe_ClassifiesInactiveEventNoMetadata(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.capabilityFromProbe(Capability{}, false, "1:0:1:EVENT", "Sky Sport 8", now, nil)

	assert.Equal(t, CapabilityStateInactiveEventFeed, cap.State)
	assert.Equal(t, "inactive_event_feed_no_media_metadata", cap.FailureReason)
}

func TestManager_CapabilityFromProbe_PreservesBitrateTruth(t *testing.T) {
	manager := NewManager(NewMemoryStore(), t.TempDir()+"/playlist.m3u", nil)
	now := time.Now().UTC()

	cap := manager.capabilityFromProbe(Capability{}, false, "1:0:1:LINEAR", "Das Erste HD", now, &vod.StreamInfo{
		Container:   "ts",
		BitrateKbps: 11234,
		Video: vod.VideoStreamInfo{
			CodecName:  "h264",
			Width:      1920,
			Height:     1080,
			FPS:        25,
			SignalFPS:  50,
			Interlaced: true,
			FieldOrder: "tt",
		},
		Audio: vod.AudioStreamInfo{
			CodecName:     "aac",
			Channels:      6,
			BitrateKbps:   384,
			SampleRate:    48000,
			ChannelLayout: "5.1(side)",
		},
	})

	assert.Equal(t, CapabilityStateOK, cap.State)
	assert.Equal(t, 11234, cap.BitrateKbps)
	assert.Equal(t, 11234, cap.BitrateMeanKbps)
	assert.Equal(t, 11234, cap.BitratePeakKbps)
	assert.Equal(t, 1, cap.BitrateSamples)
	assert.Equal(t, 50.0, cap.SignalFPS)
	assert.Equal(t, "tt", cap.FieldOrder)
	assert.Equal(t, 6, cap.AudioChannels)
	assert.Equal(t, 384, cap.AudioBitrateKbps)
	assert.Equal(t, 48000, cap.AudioSampleRate)
	assert.Equal(t, "5.1(side)", cap.AudioChannelLayout)
}

func TestNeedsExtendedMediaTruthRetry_RequiresBitrateForAdaptiveTruth(t *testing.T) {
	assert.True(t, needsExtendedMediaTruthRetry(&vod.StreamInfo{
		Container: "ts",
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "aac",
		},
	}))
	assert.False(t, needsExtendedMediaTruthRetry(&vod.StreamInfo{
		Container:   "ts",
		BitrateKbps: 6500,
		Video: vod.VideoStreamInfo{
			CodecName: "h264",
		},
		Audio: vod.AudioStreamInfo{
			CodecName: "aac",
		},
	}))
}
