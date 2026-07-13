package v3

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPlaybackEvidence_MapsCorrectly(t *testing.T) {
	now := time.Now().UnixMilli()
	input := LegacyPlanningInput{
		EvaluatedAt:        now,
		Scope:              "live",
		RequestedIntent:    "stream_start",
		SourceIdentity:     "1:0:1:1337:42:99:0:0:0:0:",
		Provenance:         "scan",
		Confidence:         "ok",
		ObservedAt:         now - 5000,
		ValidUntil:         now + 60000,
		NetworkCaptureTime: now,
		PolicyVersion:      "v1",

		Container:         "mpegts",
		VideoCodec:        "h264",
		AudioCodec:        "aac",
		Width:             1920,
		Height:            1080,
		FPS:               50,
		Interlaced:        false,
		BitrateKbps:       4000,
		BitrateConfidence: "high",

		ClientFamily:         "safari_native",
		DeviceType:           "mobile",
		CapabilityVersion:    "1.0",
		AllowTranscode:       false,
		SupportedContainers:  []string{"mp4", "hls"},
		SupportedVideoCodecs: []string{"h264", "hevc"},
		SupportedAudioCodecs: []string{"aac"},
		MaxVideoWidth:        1920,
		MaxVideoHeight:       1080,
		MaxVideoFPS:          60,
		PreferredEngine:      "native",
		SupportedEngines:     []string{"native", "hlsjs"},
		SupportsHls:          true,
		SupportsRange:        nil,

		DownlinkKbps:      5000,
		RTTMillis:         50,
		InternetValidated: true,

		HostPressureBand: "relaxed",
		AvailableEngines: []string{"hls"},
		PerformanceClass: "high",
		BenchmarkClass:   "A",

		ForceIntent:        "stream_start",
		MaxQualityRung:     "high",
		DisableTranscoding: false,
		MaxGlobalBitrate:   8000,
	}

	ev, err := BuildPlaybackEvidence(input)
	require.NoError(t, err)

	assert.Equal(t, now, ev.EvaluatedAt)
	assert.Equal(t, "live", ev.Scope)
	assert.Equal(t, "stream_start", ev.RequestedIntent)
	assert.Equal(t, "1:0:1:1337:42:99:0:0:0:0:", ev.SourceIdentity)

	assert.Equal(t, "mpegts", ev.SourceTruth.Container)
	assert.Equal(t, "h264", ev.SourceTruth.VideoCodec)
	assert.Equal(t, "aac", ev.SourceTruth.AudioCodec)

	assert.Equal(t, "safari_native", ev.ClientEvidence.Family)
	assert.False(t, ev.ClientEvidence.AllowTranscode)
	assert.ElementsMatch(t, []string{"mp4", "hls"}, ev.ClientEvidence.SupportedContainers)
	assert.Equal(t, "native", ev.ClientEvidence.PreferredEngine)

	assert.Equal(t, 5000, ev.NetworkEvidence.DownlinkKbps)
	assert.Equal(t, "relaxed", ev.HostSnapshot.PressureBand)
	assert.False(t, ev.OperatorPolicy.DisableTranscoding)

	// Ensure hashing works without panic
	hash, err := ev.Hash()
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
}
