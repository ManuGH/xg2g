package ffmpeg

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func androidTVNativeTranscodeSpec() ports.StreamSpec {
	return ports.StreamSpec{
		SessionID:    "android-tv-native-startup",
		ClientFamily: "android_tv_native",
		Mode:         ports.ModeLive,
		Format:       ports.FormatHLS,
		Source: ports.StreamSource{
			ID:   "1:0:19:132F:3EF:1:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
		Profile: ports.ProfileSpec{
			Name:           "android",
			Container:      "fmp4",
			TranscodeVideo: true,
			VideoCodec:     "h264",
			Deinterlace:    true,
		},
	}
}

func TestAndroidTVNativeStartupSkipsFPSProbeForDirectTuner(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)
	probeCalls := 0
	adapter.fpsProbeFn = func(context.Context, string) (int, string, error) {
		probeCalls++
		return 0, "", errors.New("native direct tuner must not run fps probe")
	}

	_, err := adapter.buildArgs(context.Background(), androidTVNativeTranscodeSpec(), "http://10.10.55.64:8001/live")
	require.NoError(t, err)
	assert.Zero(t, probeCalls)
}

func TestAndroidTVNativeStartupSkipsAudioProbeWithoutChangingWeb(t *testing.T) {
	adapter := &LocalAdapter{Logger: zerolog.Nop()}
	probeCalls := 0
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		probeCalls++
		return []liveAudioStream{{Index: 2, CodecType: "audio", CodecName: "mp2", Channels: 2}}, nil
	}

	native := adapter.planLiveAudioSelection(context.Background(), androidTVNativeTranscodeSpec(), "http://example.com/live")
	assert.Equal(t, []string{defaultLiveAudioMap}, native.Maps)
	assert.Zero(t, probeCalls)

	webSpec := androidTVNativeTranscodeSpec()
	webSpec.ClientFamily = "chromium_hlsjs"
	web := adapter.planLiveAudioSelection(context.Background(), webSpec, "http://example.com/live")
	assert.Equal(t, []string{"0:2?"}, web.Maps)
	assert.Equal(t, 1, probeCalls, "web audio selection must retain its compatibility probe")
}

func TestAndroidTVNativeTranscodeUsesOneSecondSegmentsOnly(t *testing.T) {
	adapter := &LocalAdapter{SegmentSeconds: 2, ReadySegments: 1}

	nativeLayout, err := adapter.planLiveSegmentLayout(androidTVNativeTranscodeSpec())
	require.NoError(t, err)
	assert.Equal(t, 1, nativeLayout.segmentDurationSec)

	webSpec := androidTVNativeTranscodeSpec()
	webSpec.ClientFamily = "chromium_hlsjs"
	webLayout, err := adapter.planLiveSegmentLayout(webSpec)
	require.NoError(t, err)
	assert.Equal(t, 2, webLayout.segmentDurationSec)

	copySpec := androidTVNativeTranscodeSpec()
	copySpec.Profile.TranscodeVideo = false
	copyLayout, err := adapter.planLiveSegmentLayout(copySpec)
	require.NoError(t, err)
	assert.Equal(t, 2, copyLayout.segmentDurationSec)
}
