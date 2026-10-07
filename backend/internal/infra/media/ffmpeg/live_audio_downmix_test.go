// Copyright (c) 2026 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0
// Since v2.0.0, this software is restricted to non-commercial use only.

package ffmpeg

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanLiveAudio_BroadcastDownmixFilter(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	// Stream 1 (deu): 6 channels (5.1 surround)
	// Stream 2 (eng): 2 channels (stereo)
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return []liveAudioStream{
			{Index: 2, ID: "0x203", CodecType: "audio", CodecName: "ac3", Channels: 6, Tags: map[string]string{"language": "deu"}},
			{Index: 3, ID: "0x204", CodecType: "audio", CodecName: "ac3", Channels: 2, Tags: map[string]string{"language": "eng"}},
		}, nil
	}

	pmt := []ports.LiveAudioTrack{
		{PID: 0x203, Codec: "ac3", Channels: 6, Language: "deu"},
		{PID: 0x204, Codec: "ac3", Channels: 2, Language: "eng"},
	}

	spec := ports.StreamSpec{
		SessionID: "test-downmix",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:             "av1_hw",
			Container:        "fmp4",
			VideoCodec:       "av1",
			VideoSourceCodec: "h264",
			TranscodeVideo:   true,
			AudioBitrateK:    192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:93:2:85:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	sel := adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)
	require.True(t, sel.IsMultiAudio)
	require.Len(t, sel.Maps, 2)

	// Stream 0 (5.1 deu) should have -filter:a:0 with BroadcastDownmixFilter chained with aresample
	filter0, ok0 := valueAfter(sel.AudioArgs, "-filter:a:0")
	require.True(t, ok0, "expected -filter:a:0 for 6-channel input downmixed to stereo; args: %v", sel.AudioArgs)
	assert.Equal(t, BroadcastDownmixFilter+",aresample=async=1", filter0)

	// Stream 1 (2.0 eng) should have -filter:a:1 with aresample, but NOT BroadcastDownmixFilter
	filter1, ok1 := valueAfter(sel.AudioArgs, "-filter:a:1")
	require.True(t, ok1, "expected -filter:a:1 with aresample; args: %v", sel.AudioArgs)
	assert.Equal(t, "aresample=async=1", filter1)
}

func TestPlanLiveAudio_SingleAudio_BroadcastDownmixFilter(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	// Only 1 stream: 6 channels (5.1 surround)
	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return []liveAudioStream{
			{Index: 2, ID: "0x203", CodecType: "audio", CodecName: "ac3", Channels: 6, Tags: map[string]string{"language": "deu"}},
		}, nil
	}

	pmt := []ports.LiveAudioTrack{
		{PID: 0x203, Codec: "ac3", Channels: 6, Language: "deu"},
	}

	spec := ports.StreamSpec{
		SessionID: "test-downmix-single",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:             "av1_hw",
			Container:        "fmp4",
			VideoCodec:       "av1",
			VideoSourceCodec: "h264",
			TranscodeVideo:   true,
			AudioBitrateK:    192,
		},
		Source: ports.StreamSource{
			ID:   "1:0:19:93:2:85:C00000:0:0:0",
			Type: ports.SourceTuner,
		},
	}

	sel := adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)
	assert.False(t, sel.IsMultiAudio)

	filter, ok := valueAfter(sel.AudioArgs, "-af")
	require.True(t, ok, "expected -af for single 6-channel input downmixed to stereo; args: %v", sel.AudioArgs)
	assert.Equal(t, BroadcastDownmixFilter+",aresample=async=1", filter)
}

func TestPlanLiveAudio_AACPassthroughFMP4_AppliesBSF(t *testing.T) {
	adapter := NewLocalAdapter(
		"ffmpeg", "ffprobe", t.TempDir(), nil, zerolog.New(io.Discard),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	adapter.liveAudioProbeFn = func(context.Context, string) ([]liveAudioStream, error) {
		return []liveAudioStream{
			{Index: 1, ID: "0x101", CodecType: "audio", CodecName: "aac", Channels: 2, Tags: map[string]string{"language": "eng"}},
		}, nil
	}

	pmt := []ports.LiveAudioTrack{
		{PID: 0x101, Codec: "aac", Channels: 2, Language: "eng"},
	}

	spec := ports.StreamSpec{
		SessionID: "test-aac-fmp4",
		Mode:      ports.ModeLive,
		Format:    ports.FormatHLS,
		Quality:   ports.QualityStandard,
		Profile: model.ProfileSpec{
			Name:           "safari",
			Container:      "fmp4",
			TranscodeVideo: false,
			AudioMode:      "copy",
			AudioCodec:     "aac",
		},
		Source: ports.StreamSource{
			ID:   "iptv_test",
			Type: ports.SourceTuner,
		},
	}

	sel := adapter.planLiveAudioSelection(context.Background(), spec, spec.Source.ID, pmt)
	assert.False(t, sel.IsMultiAudio)
	assert.Contains(t, sel.AudioArgs, "-c:a")
	assert.Contains(t, sel.AudioArgs, "copy")
	assert.Contains(t, sel.AudioArgs, "-bsf:a")
	assert.Contains(t, sel.AudioArgs, "aac_adtstoasc")
}

