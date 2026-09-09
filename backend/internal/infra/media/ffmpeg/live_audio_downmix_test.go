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

	// Stream 0 (5.1 deu) should have -filter:a:0 with BroadcastDownmixFilter
	filter0, ok0 := valueAfter(sel.AudioArgs, "-filter:a:0")
	require.True(t, ok0, "expected -filter:a:0 for 6-channel input downmixed to stereo; args: %v", sel.AudioArgs)
	assert.Equal(t, BroadcastDownmixFilter, filter0)

	// Stream 1 (2.0 eng) should NOT have -filter:a:1 because it is already stereo
	_, ok1 := valueAfter(sel.AudioArgs, "-filter:a:1")
	assert.False(t, ok1, "2-channel input must not receive -filter:a:1; args: %v", sel.AudioArgs)
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

	filter, ok := valueAfter(sel.AudioArgs, "-filter:a")
	require.True(t, ok, "expected -filter:a for single 6-channel input downmixed to stereo; args: %v", sel.AudioArgs)
	assert.Equal(t, BroadcastDownmixFilter, filter)
}
