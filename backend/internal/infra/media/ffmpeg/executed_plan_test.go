// Copyright (c) 2025 ManuGH
// Licensed under the PolyForm Noncommercial License 1.0.0

package ffmpeg

import (
	"testing"

	"github.com/ManuGH/xg2g/internal/domain/session/ports"
	"github.com/ManuGH/xg2g/internal/pipeline/profiles"
)

func TestExecutedFFmpegPlanFromArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want ports.ExecutedFFmpegPlan
	}{
		{
			name: "copy mpegts (Safari live, mp2->aac)",
			args: []string{
				"-i", "http://tuner/orf1",
				"-c:v", "copy",
				"-c:a", "aac", "-b:a", "256k", "-ac", "2", "-ar", "48000",
				"-f", "hls",
				"-hls_segment_type", "mpegts",
				"-hls_segment_filename", "/var/lib/xg2g/hls/sessions/x/seg_%06d.ts",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "copy fmp4 (native HLS)",
			args: []string{
				"-i", "http://tuner/orf1",
				"-c:v", "copy",
				"-c:a", "aac",
				"-f", "hls",
				"-hls_segment_type", "fmp4",
				"-hls_segment_filename", "/x/seg_%06d.m4s",
				"-hls_fmp4_init_filename", "init.mp4",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "implicit segment type via .m4s filename",
			args: []string{
				"-c:v", "copy", "-c:a", "aac",
				"-hls_segment_filename", "/x/seg_%06d.m4s",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "cpu h264 transcode",
			args: []string{
				"-c:v", "libx264", "-preset", "veryfast", "-crf", "20",
				"-c:a", "aac",
				"-hls_segment_type", "mpegts",
				"-hls_segment_filename", "/x/seg_%06d.ts",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "none",
				VideoMode: "transcode", VideoCodec: "h264",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "vaapi full (decode+encode on GPU)",
			args: []string{
				"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi",
				"-vaapi_device", "/dev/dri/renderD128",
				"-i", "http://tuner/x",
				"-c:v", "h264_vaapi",
				"-c:a", "aac",
				"-hls_segment_type", "fmp4",
				"-hls_segment_filename", "/x/seg_%06d.m4s",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "vaapi",
				VideoMode: "transcode", VideoCodec: "h264",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "vaapi encode-only (software decode, gpu encode)",
			args: []string{
				"-vaapi_device", "/dev/dri/renderD128",
				"-i", "http://tuner/x",
				"-vf", "bwdif,format=nv12,hwupload",
				"-c:v", "h264_vaapi",
				"-c:a", "aac",
				"-hls_segment_type", "mpegts",
				"-hls_segment_filename", "/x/seg_%06d.ts",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "vaapi_encode_only",
				VideoMode: "transcode", VideoCodec: "h264",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "nvenc hevc",
			args: []string{
				"-c:v", "hevc_nvenc",
				"-c:a", "aac",
				"-hls_segment_type", "mpegts",
				"-hls_segment_filename", "/x/seg_%06d.ts",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "nvenc",
				VideoMode: "transcode", VideoCodec: "hevc",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "av1 vaapi encode-only with audio copy",
			args: []string{
				"-vaapi_device", "/dev/dri/renderD128",
				"-c:v", "av1_vaapi",
				"-c:a", "copy",
				"-hls_segment_type", "mpegts",
				"-hls_segment_filename", "/x/seg_%06d.ts",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mpegts", Packaging: "ts", HWAccel: "vaapi_encode_only",
				VideoMode: "transcode", VideoCodec: "av1",
				AudioMode: "copy", AudioCodec: "copy",
			},
		},
		{
			// LL-HLS pipe mode: no hls muxer flags at all — ffmpeg streams one
			// fragmented MP4 to stdout for the in-process cmaf segmenter. Must
			// classify as fmp4, not fall back to mpegts (that fallback fired a
			// spurious plan_mismatch on every LL-HLS session).
			name: "llhls cmaf pipe (fragmented mp4 on stdout)",
			args: []string{
				"-vaapi_device", "/dev/dri/renderD128",
				"-i", "http://tuner/x",
				"-c:v", "av1_vaapi",
				"-c:a", "aac",
				"-f", "mp4",
				"-movflags", "empty_moov+default_base_moof+skip_trailer+frag_keyframe",
				"-frag_duration", "500000",
				"-flush_packets", "1",
				"pipe:1",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "vaapi_encode_only",
				VideoMode: "transcode", VideoCodec: "av1",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			// An input-side -f names the demuxer of the FOLLOWING -i and must
			// not leak into the output classification.
			name: "input -f does not leak into output format",
			args: []string{
				"-f", "mpegts",
				"-i", "http://tuner/x",
				"-c:v", "copy",
				"-c:a", "aac",
				"-f", "mp4",
				"-movflags", "empty_moov+frag_keyframe",
				"pipe:1",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "fmp4", Packaging: "fmp4", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
		{
			name: "plain unfragmented mp4 output stays mp4",
			args: []string{
				"-i", "http://tuner/x",
				"-c:v", "copy",
				"-c:a", "aac",
				"-f", "mp4",
				"/x/out.mp4",
			},
			want: ports.ExecutedFFmpegPlan{
				Container: "mp4", Packaging: "mp4", HWAccel: "none",
				VideoMode: "copy", VideoCodec: "copy",
				AudioMode: "transcode", AudioCodec: "aac",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := executedFFmpegPlanFromArgs(tc.args)
			if got != tc.want {
				t.Fatalf("executedFFmpegPlanFromArgs mismatch\n got: %+v\nwant: %+v", got, tc.want)
			}
		})
	}
}

func TestResolvedExecutedHWAccel(t *testing.T) {
	cases := []struct {
		name string
		in   codecPlan
		want string
	}{
		{"copy/none", codecPlan{useHW: false}, ""},
		{"cpu transcode/none", codecPlan{useHW: false, hwBackend: profiles.GPUBackendNone}, ""},
		{"vaapi full", codecPlan{useHW: true, hwBackend: profiles.GPUBackendVAAPI, fullVAAPI: true}, "vaapi"},
		{"vaapi encode-only (downgraded full / av1)", codecPlan{useHW: true, hwBackend: profiles.GPUBackendVAAPI, fullVAAPI: false}, "vaapi_encode_only"},
		{"nvenc", codecPlan{useHW: true, hwBackend: profiles.GPUBackendNVENC}, "nvenc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvedExecutedHWAccel(tc.in); got != tc.want {
				t.Fatalf("resolvedExecutedHWAccel(%+v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
