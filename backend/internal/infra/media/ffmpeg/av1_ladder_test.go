// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLadderTestAdapter(t *testing.T, tracks []liveAudioStream) *LocalAdapter {
	t.Helper()
	adapter := newABRTestAdapter(t)
	adapter.VaapiDevice = "/dev/dri/renderD128"
	adapter.detector.vaapiEncoders = map[string]bool{"av1_vaapi": true, "hevc_vaapi": true, "h264_vaapi": true}
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return tracks, nil
	}
	return adapter
}

func ladderSpec(family string) ports.StreamSpec {
	return ports.StreamSpec{
		SessionID:    "sess-av1-ladder",
		ClientFamily: family,
		Mode:         ports.ModeLive,
		Format:       ports.FormatHLS,
		Source:       ports.StreamSource{Type: ports.SourceURL, ID: "stream1"},
		Profile: ports.ProfileSpec{
			Name:             "av1_hw",
			Container:        "fmp4",
			VideoCodec:       "av1",
			VideoSourceCodec: "h264",
			TranscodeVideo:   true,
			HWAccel:          "vaapi",
			VideoMaxRateK:    32000,
			VideoBufSizeK:    64000,
			AudioBitrateK:    192,
		},
	}
}

var (
	ladderTwoTracks = []liveAudioStream{
		{Index: 1, ID: "0x781", CodecType: "audio", CodecName: "ac3", Channels: 6, ChannelLayout: "5.1(side)", Tags: map[string]string{"language": "deu"}},
		{Index: 2, ID: "0x783", CodecType: "audio", CodecName: "mp2", Channels: 2, Tags: map[string]string{"language": "mul"}},
	}
	ladderOneTrack = []liveAudioStream{
		{Index: 1, ID: "0x6f8", CodecType: "audio", CodecName: "mp2", Channels: 2, Tags: map[string]string{"language": "deu"}},
	}
)

func buildLadderArgs(t *testing.T, tracks []liveAudioStream, spec ports.StreamSpec) []string {
	t.Helper()
	plan, err := newLadderTestAdapter(t, tracks).buildArgsWithPlan(context.Background(), spec, "http://localhost:8080/stream", nil)
	require.NoError(t, err)
	return plan.args
}

func indexOfPair(args []string, key, value string) int {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return i
		}
	}
	return -1
}

func TestAV1Ladder_MultiAudioIOS(t *testing.T) {
	t.Setenv("XG2G_LIVE_AV1_LADDER_LOW_K", "8000")
	args := buildLadderArgs(t, ladderTwoTracks, ladderSpec("ios_safari"))

	assert.NotContains(t, args, "-vf", "the chain moves into filter_complex")
	assert.Equal(t, -1, indexOfPair(args, "-map", "0:v:0?"))

	fc, ok := valueAfter(args, "-filter_complex")
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(fc, "[0:v:0]"), fc)
	assert.True(t, strings.HasSuffix(fc, ",split=2[vlo][vhi]"), fc)

	lo, hi := indexOfPair(args, "-map", "[vlo]"), indexOfPair(args, "-map", "[vhi]")
	require.GreaterOrEqual(t, lo, 0)
	require.Greater(t, hi, lo, "low rung is v:0")
	for i := 0; i < len(args)-1; i++ {
		if args[i] == "-map" && !strings.HasPrefix(args[i+1], "[v") {
			assert.Greater(t, i, hi, "video rungs stay the first output streams")
		}
	}

	regular := indexOfPair(args, "-b:v", "32000k")
	low := indexOfPair(args, "-b:v:0", "8000k")
	require.GreaterOrEqual(t, regular, 0)
	require.Greater(t, low, regular, "per-stream override must follow the unindexed rate")
	assert.GreaterOrEqual(t, indexOfPair(args, "-maxrate:v:0", "8000k"), 0)
	assert.GreaterOrEqual(t, indexOfPair(args, "-bufsize:v:0", "16000k"), 0)

	vsm, ok := valueAfter(args, "-var_stream_map")
	require.True(t, ok)
	assert.Equal(t, "v:0,agroup:audio v:1,agroup:audio,default:yes a:0,agroup:audio,language:de,default:yes a:1,agroup:audio,language:mul,default:no", vsm)
	master, _ := valueAfter(args, "-master_pl_name")
	assert.Equal(t, "index.m3u8", master)
	assert.True(t, strings.HasSuffix(args[len(args)-1], "stream_%v.m3u8"))
}

func TestAV1Ladder_SingleAudioBecomesAudioGroup(t *testing.T) {
	t.Setenv("XG2G_LIVE_AV1_LADDER_LOW_K", "8000")
	args := buildLadderArgs(t, ladderOneTrack, ladderSpec("ios_safari"))

	vsm, ok := valueAfter(args, "-var_stream_map")
	require.True(t, ok, "the single track must leave the muxed layout")
	assert.Equal(t, "v:0,agroup:audio v:1,agroup:audio,default:yes a:0,agroup:audio,language:de,default:yes", vsm)
	segments, _ := valueAfter(args, "-hls_segment_filename")
	assert.Contains(t, segments, "seg_%v_%06d.m4s")
	initName, _ := valueAfter(args, "-hls_fmp4_init_filename")
	assert.Contains(t, initName, "init_%v.mp4")
	assert.True(t, strings.HasSuffix(args[len(args)-1], "stream_%v.m3u8"))
}

func TestAV1Ladder_OffKeepsSingleRendition(t *testing.T) {
	cases := []struct {
		name   string
		env    string
		mutate func(*ports.StreamSpec)
	}{
		{name: "unset", env: ""},
		{name: "non-iOS client", env: "8000", mutate: func(s *ports.StreamSpec) { s.ClientFamily = "android_tv_native" }},
		{name: "HEVC output", env: "8000", mutate: func(s *ports.StreamSpec) { s.Profile.VideoCodec = "hevc"; s.Profile.Name = "safari_hevc_hw" }},
		{name: "low rung not below ceiling", env: "8000", mutate: func(s *ports.StreamSpec) { s.Profile.VideoMaxRateK = 8000 }},
		{name: "explicit QP", env: "8000", mutate: func(s *ports.StreamSpec) { s.Profile.VideoQP = 30 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("XG2G_LIVE_AV1_LADDER_LOW_K", tc.env)
			spec := ladderSpec("ios_safari")
			if tc.mutate != nil {
				tc.mutate(&spec)
			}
			args := buildLadderArgs(t, ladderTwoTracks, spec)
			assert.GreaterOrEqual(t, indexOfPair(args, "-map", "0:v:0?"), 0)
			assert.Equal(t, -1, indexOfPair(args, "-map", "[vlo]"))
			assert.False(t, slices.Contains(args, "-b:v:0"))
			if vsm, ok := valueAfter(args, "-var_stream_map"); ok {
				assert.True(t, strings.HasPrefix(vsm, "v:0,agroup:audio,default:yes a:"), vsm)
			}
		})
	}
}

func TestAV1Ladder_AMDKeepsTargetHeadroom(t *testing.T) {
	args := av1Ladder{lowRateK: 8000}.appendLowRungRateArgs(nil, AdapterConfig{GPUVendor: "amd"})
	assert.Equal(t, []string{"-b:v:0", "6000k", "-maxrate:v:0", "8000k", "-bufsize:v:0", "16000k"}, args)
}
