package ffmpeg

import (
	"context"
	"testing"
	"time"

	"github.com/ManuGH/xg2g/internal/domain/session/model"
	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/domain/vod"
	"github.com/ManuGH/xg2g/internal/pipeline/scan"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// characterizationTest defines the FFmpeg adapter input and expected executed plan output
type characterizationTest struct {
	name      string
	spec      ports.StreamSpec
	sourceCap scan.Capability
	want      ports.ExecutedFFmpegPlan
}

func runCharacterizationTest(t *testing.T, tc characterizationTest) {
	adapter := NewLocalAdapter(
		"ffmpeg", "", t.TempDir(), nil, zerolog.Nop(),
		"", "", 0, 0, false, 2*time.Second, 6, 0, 0, "",
	)

	// Inject the source capability into the stream probe
	adapter.streamProbeFn = func(ctx context.Context, inputURL string) (*vod.StreamInfo, error) {
		return &vod.StreamInfo{
			Container: tc.sourceCap.Container,
			Video: vod.VideoStreamInfo{
				CodecName:  tc.sourceCap.VideoCodec,
				Interlaced: tc.sourceCap.Interlaced,
			},
		}, nil
	}

	args, err := adapter.buildArgs(context.Background(), tc.spec, tc.spec.Source.ID)
	require.NoError(t, err)

	got := executedFFmpegPlanFromArgs(args)
	require.Equal(t, tc.want, got)
}

func TestFFmpegPlanner_Characterization(t *testing.T) {
	cases := []characterizationTest{
		{
			name:      "1_Safari_Native_H264",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "safari",
					TranscodeVideo: false,
					Container:      "mpegts",
				},
				Source: ports.StreamSource{ID: "http://tuner/1", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "copy", AudioCodec: "copy",
			},
		},
		{
			name:      "2_Safari_Native_HEVC_4K",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "hevc", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "safari",
					TranscodeVideo: false,
					Container:      "fmp4",
					VideoCodec:     "hevc",
				},
				Source: ports.StreamSource{ID: "http://tuner/2", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "copy", AudioCodec: "copy",
			},
		},
		{
			name:      "3_iOS_Safari",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "safari",
					TranscodeVideo: false,
					Container:      "mpegts",
				},
				Source: ports.StreamSource{ID: "http://tuner/3", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "copy", AudioCodec: "copy",
			},
		},
		{
			name:      "4_Chromium_HLSJS",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "hlsjs",
					TranscodeVideo: false,
					Container:      "mpegts",
				},
				Source: ports.StreamSource{ID: "http://tuner/4", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "copy", AudioCodec: "copy",
			},
		},
		{
			name:      "5_Constrained_WAN_Fallback",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "high",
					TranscodeVideo: true,
					Container:      "mpegts",
					VideoCodec:     "libx264",
				},
				Source: ports.StreamSource{ID: "http://tuner/5", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "transcode", VideoCodec: "h264",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name:      "6_Dirty_DVB_Fallback",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: true},
			spec: ports.StreamSpec{
				Mode:   ports.ModeLive,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "repair",
					TranscodeVideo: true,
					Container:      "mpegts",
					VideoCodec:     "libx264",
					Deinterlace:    true,
				},
				Source: ports.StreamSource{ID: "http://tuner/6", Type: ports.SourceURL},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "transcode", VideoCodec: "h264",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name:      "7_Recording_Playback",
			sourceCap: scan.Capability{Container: "mpegts", VideoCodec: "h264", Interlaced: false},
			spec: ports.StreamSpec{
				Mode:   ports.ModeRecording,
				Format: ports.FormatHLS,
				Profile: model.ProfileSpec{
					Name:           "recording_hlsjs",
					TranscodeVideo: false,
					Container:      "mpegts",
				},
				Source: ports.StreamSource{ID: "/recordings/1.ts", Type: ports.SourceFile},
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "", AudioCodec: "",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runCharacterizationTest(t, tc)
		})
	}
}
